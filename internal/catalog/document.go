package catalog

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
)

// Document reads runtime metadata from the same catalog as search. Indexed
// records are stored once and restored into their canonical source locations.
func (d *DB) Document(ctx context.Context, member string) (map[string]any, error) {
	var data []byte
	if err := d.db.QueryRowContext(ctx, "SELECT data FROM runtime_documents WHERE member=?", member).Scan(&data); err != nil {
		return nil, fmt.Errorf("runtime metadata %s: %w; regenerate the SQLite catalog with tools/gts bundle sqlite", member, err)
	}
	raw, err := inflateDocument(data)
	if err != nil {
		return nil, err
	}
	var document map[string]any
	if err = json.Unmarshal(raw, &document); err != nil {
		return nil, err
	}
	if document == nil {
		return nil, fmt.Errorf("runtime metadata %s is not an object", member)
	}
	rows, err := d.db.QueryContext(ctx, "SELECT s.pointer,p.data FROM item_sources s LEFT JOIN item_payloads p ON p.item_id=s.item_id WHERE s.member=? ORDER BY s.item_id", member)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pointer string
		if err = rows.Scan(&pointer, &data); err != nil {
			return nil, err
		}
		if pointer == "" {
			continue
		}
		raw, err = inflateDocument(data)
		if err != nil {
			return nil, err
		}
		var value any
		if err = json.Unmarshal(raw, &value); err != nil {
			return nil, err
		}
		if err = setDocumentPointer(document, pointer, value); err != nil {
			return nil, fmt.Errorf("%s: %w", member, err)
		}
	}
	return document, rows.Err()
}
func inflateDocument(data []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	defer r.Close()
	return io.ReadAll(r)
}
func setDocumentPointer(document map[string]any, pointer string, value any) error {
	if !strings.HasPrefix(pointer, "/") {
		return fmt.Errorf("invalid source pointer %q", pointer)
	}
	parts := strings.Split(pointer[1:], "/")
	var target any = document
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
					return fmt.Errorf("invalid source pointer %q", pointer)
				}
				j++
			}
		}
		part = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
		last := i == len(parts)-1
		switch node := target.(type) {
		case map[string]any:
			if _, exists := node[part]; !exists {
				return fmt.Errorf("invalid source pointer %q", pointer)
			}
			if last {
				node[part] = value
				return nil
			}
			target = node[part]
		case []any:
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 || n >= len(node) || strconv.Itoa(n) != part {
				return fmt.Errorf("invalid source pointer %q", pointer)
			}
			if last {
				node[n] = value
				return nil
			}
			target = node[n]
		default:
			return fmt.Errorf("invalid source pointer %q", pointer)
		}
	}
	return nil
}
