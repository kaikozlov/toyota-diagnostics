package catalog

import (
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
	return decodeStoredJSON(raw)
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

// containingPart reads pointer from the closest stored part that contains
// it, reporting whether there is one. With keys, pointer must be an object
// and only those members are returned; when the part is that object, the
// other members are checked as JSON but never built.
func (d *DB) containingPart(ctx context.Context, member, pointer string, keys []string) (any, bool, error) {
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
	if errors.Is(err, sql.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	root, err := d.fullDocument(ctx, member)
	if err != nil {
		return nil, false, err
	}
	marker, err := d.documentPointerValue(ctx, root, ancestor)
	if err != nil {
		return nil, false, err
	}
	if err = validatePartMarker(marker, partID, ancestor); err != nil {
		return nil, false, err
	}
	relative := strings.TrimPrefix(pointer, ancestor)
	if keys != nil && relative == "" {
		value, err := storedMembers(packed, keys)
		if err != nil {
			return nil, false, fmt.Errorf("%s%s: %w", member, ancestor, err)
		}
		return value, true, unresolvedParts(value)
	}
	value, err := storedJSON(packed)
	if err != nil {
		return nil, false, fmt.Errorf("%s%s: %w", member, ancestor, err)
	}
	if relative != "" {
		if value, err = d.documentPointerValue(ctx, value, relative); err != nil {
			return nil, false, err
		}
	}
	if keys != nil {
		if value, err = selectMembers(value, keys); err != nil {
			return nil, false, err
		}
	}
	if err = unresolvedParts(value); err != nil {
		return nil, false, err
	}
	return value, true, nil
}

// selectMembers picks the named members of a decoded object.
func selectMembers(value any, keys []string) (map[string]any, error) {
	object, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("stored value is not an object")
	}
	selected := make(map[string]any, len(keys))
	for _, key := range keys {
		if v, ok := object[key]; ok {
			selected[key] = v
		}
	}
	return selected, nil
}

// sourceValue selects the closest stored semantic part before decoding JSON.
// Reading one ECU must not decode the complete regional inventory.
func (d *DB) sourceValue(ctx context.Context, member, pointer string) (any, error) {
	if pointer != "" {
		if _, err := splitDocumentPointer(pointer); err != nil {
			return nil, err
		}
		value, found, err := d.containingPart(ctx, member, pointer, nil)
		if err != nil || found {
			// Semantic parts do not overlap. Once a containing part is found,
			// no descendant-part scan or unrelated document read is needed.
			return value, err
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
			// Sibling parts share a parent; resolving it once per part would
			// rescan a large parent for every child.
			parents := map[string]any{}
			for _, p := range selected {
				relative := strings.TrimPrefix(p.pointer, pointer)
				cut := strings.LastIndex(relative, "/")
				parent, ok := parents[relative[:cut]]
				if !ok {
					parent = value
					if cut > 0 {
						parent, err = d.documentPointerValue(ctx, value, relative[:cut])
					}
					if err == nil {
						parent, err = d.resolveReferences(ctx, parent, relative)
					}
					if err != nil {
						return nil, err
					}
					parents[relative[:cut]] = parent
				}
				last, err := splitDocumentPointer(relative[cut:])
				if err != nil {
					return nil, err
				}
				expected, err := pointerChild(parent, last[0], relative)
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
