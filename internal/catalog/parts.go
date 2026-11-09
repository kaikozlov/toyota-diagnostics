package catalog

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

func storedJSON(packed []byte) (any, error) {
	raw, err := inflateDocument(packed)
	if err != nil {
		return nil, err
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err = decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func cloneStoredJSON(value any) any {
	switch node := value.(type) {
	case map[string]any:
		out := make(map[string]any, len(node))
		for k, v := range node {
			out[k] = cloneStoredJSON(v)
		}
		return out
	case []any:
		out := make([]any, len(node))
		for i, v := range node {
			out[i] = cloneStoredJSON(v)
		}
		return out
	default:
		return value
	}
}

// sourceValue selects the closest stored semantic part before decoding JSON.
// Reading one ECU must not decode the complete regional inventory.
func (d *DB) sourceValue(ctx context.Context, member, pointer string) (any, error) {
	if pointer != "" {
		if _, err := splitDocumentPointer(pointer); err != nil {
			return nil, err
		}
	}
	var value any
	if pointer != "" {
		args := []any{member}
		marks := []string{}
		for ancestor := pointer; ancestor != ""; ancestor = ancestor[:strings.LastIndex(ancestor, "/")] {
			args = append(args, ancestor)
			marks = append(marks, "?")
		}
		var ancestor string
		var partID int64
		var packed []byte
		query := `SELECT p.id,p.pointer,r.data FROM source_documents d
JOIN source_document_parts r ON r.document_id=d.id
JOIN source_pointers p ON p.id=r.pointer_id
WHERE d.member=? AND p.pointer IN (` + strings.Join(marks, ",") + `) ORDER BY length(p.pointer) DESC LIMIT 1`
		err := d.db.QueryRowContext(ctx, query, args...).Scan(&partID, &ancestor, &packed)
		if err == nil {
			root, err := d.fullDocument(ctx, member)
			if err != nil {
				return nil, err
			}
			marker, err := d.documentPointerValue(ctx, root, ancestor)
			if err != nil {
				return nil, err
			}
			if err = validatePartMarker(marker, partID, ancestor); err != nil {
				return nil, err
			}
			value, err = storedJSON(packed)
			if err != nil {
				return nil, fmt.Errorf("%s%s: %w", member, ancestor, err)
			}
			relative := strings.TrimPrefix(pointer, ancestor)
			if relative != "" {
				value, err = d.documentPointerValue(ctx, value, relative)
				if err != nil {
					return nil, err
				}
			}
			if err = unresolvedParts(value); err != nil {
				return nil, err
			}
			// Semantic parts do not overlap. Once a containing part is found,
			// no descendant-part scan or unrelated document read is needed.
			return value, nil
		} else if !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
	}
	value, err := d.fullDocument(ctx, member)
	if err != nil {
		return nil, err
	}
	if pointer != "" {
		value, err = d.documentPointerValue(ctx, value, pointer)
		if err != nil {
			return nil, err
		}
	}
	{
		// Only descendants of the selected subtree are read; unrelated corrupt or
		// large sections cannot affect a targeted item inspection.
		prefix := pointer + "/"
		rows, err := d.db.QueryContext(ctx, `SELECT p.id,p.pointer,r.data FROM source_documents d
JOIN source_document_parts r ON r.document_id=d.id JOIN source_pointers p ON p.id=r.pointer_id
WHERE d.member=? AND substr(p.pointer,1,?)=? ORDER BY p.pointer`, member, len(prefix), prefix)
		if err != nil {
			return nil, err
		}
		type part struct {
			id      int64
			pointer string
			packed  []byte
		}
		var selected []part
		for rows.Next() {
			var p part
			if err = rows.Scan(&p.id, &p.pointer, &p.packed); err != nil {
				rows.Close()
				return nil, err
			}
			selected = append(selected, p)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(selected) > 0 {
			value = cloneStoredJSON(value)
			wrapper := map[string]any{"value": value}
			for _, p := range selected {
				relative := strings.TrimPrefix(p.pointer, pointer)
				expected, err := d.documentPointerValue(ctx, value, relative)
				if err != nil {
					return nil, err
				}
				if err = validatePartMarker(expected, p.id, p.pointer); err != nil {
					return nil, err
				}
				partValue, err := storedJSON(p.packed)
				if err != nil {
					return nil, fmt.Errorf("%s%s: %w", member, p.pointer, err)
				}
				if err = setDocumentPointer(wrapper, "/value"+relative, partValue); err != nil {
					return nil, err
				}
			}
			value = wrapper["value"]
		}
	}
	if err := unresolvedParts(value); err != nil {
		return nil, fmt.Errorf("%s%s: %w", member, pointer, err)
	}
	return value, nil
}

func validatePartMarker(value any, id int64, pointer string) error {
	marker, ok := value.(map[string]any)
	if !ok || len(marker) != 1 {
		return fmt.Errorf("missing document part marker at %s", pointer)
	}
	number, ok := marker["$part"].(json.Number)
	if !ok {
		return fmt.Errorf("invalid document part marker at %s", pointer)
	}
	actual, err := number.Int64()
	if err != nil || actual != id {
		return fmt.Errorf("document part identity mismatch at %s", pointer)
	}
	return nil
}

func unresolvedParts(value any) error {
	switch node := value.(type) {
	case map[string]any:
		if id, ok := node["$part"]; ok && len(node) == 1 {
			return fmt.Errorf("missing document part %v", id)
		}
		for _, v := range node {
			if err := unresolvedParts(v); err != nil {
				return err
			}
		}
	case []any:
		for _, v := range node {
			if err := unresolvedParts(v); err != nil {
				return err
			}
		}
	}
	return nil
}
