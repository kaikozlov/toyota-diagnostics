package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Document reads one source document with every stored reference expanded.
// The result is an independent copy using runtime JSON numbers.
func (d *DB) Document(ctx context.Context, member string) (map[string]any, error) {
	value, err := d.sourceValue(ctx, member, "")
	if err != nil {
		return nil, err
	}
	value, err = d.runtimeValue(ctx, value)
	if err != nil {
		return nil, err
	}
	document, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("catalog document %s is not an object", member)
	}
	return document, nil
}

// runtimeValue expands stored references and converts numbers to the runtime
// representation. Stored values are never modified.
func (d *DB) runtimeValue(ctx context.Context, value any) (any, error) {
	value, err := d.expandMetadata(ctx, value, make(referenceStack))
	if err != nil {
		return nil, err
	}
	return runtimeJSONValue(value)
}

func (d *DB) documentError(member string, err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("catalog has no document %q", member)
	}
	return fmt.Errorf("catalog document %s: %w", member, err)
}

func splitDocumentPointer(pointer string) ([]string, error) {
	if !strings.HasPrefix(pointer, "/") {
		return nil, fmt.Errorf("invalid source pointer %q", pointer)
	}
	parts := strings.Split(pointer[1:], "/")
	out := make([]string, len(parts))
	for i, part := range parts {
		for j := 0; j < len(part); j++ {
			if part[j] == '~' {
				if j+1 >= len(part) || (part[j+1] != '0' && part[j+1] != '1') {
					return nil, fmt.Errorf("invalid source pointer %q", pointer)
				}
				j++
			}
		}
		out[i] = strings.ReplaceAll(strings.ReplaceAll(part, "~1", "/"), "~0", "~")
	}
	return out, nil
}

func setDocumentPointer(document map[string]any, pointer string, value any) error {
	parts, err := splitDocumentPointer(pointer)
	if err != nil {
		return err
	}
	current := any(document)
	for i, part := range parts {
		last := i == len(parts)-1
		switch node := current.(type) {
		case map[string]any:
			if last {
				if _, exists := node[part]; !exists {
					return fmt.Errorf("invalid source pointer %q", pointer)
				}
				node[part] = value
				return nil
			}
			child, exists := node[part]
			if !exists {
				return fmt.Errorf("invalid source pointer %q", pointer)
			}
			current = child
		case []any:
			n, ok := pointerIndex(part)
			if !ok || n >= len(node) {
				return fmt.Errorf("invalid source pointer %q", pointer)
			}
			if last {
				node[n] = value
				return nil
			}
			current = node[n]
		default:
			return fmt.Errorf("invalid source pointer %q", pointer)
		}
	}
	return nil
}

func pointerIndex(part string) (int, bool) {
	n, err := strconv.Atoi(part)
	if err != nil || n < 0 || strconv.Itoa(n) != part {
		return 0, false
	}
	return n, true
}
