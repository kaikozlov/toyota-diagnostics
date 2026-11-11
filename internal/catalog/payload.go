package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

// Keep at most one immutable, unexpanded document, not the entire corpus.
// Provenance expansion happens only after selecting the requested item.
const maxCachedDocumentBytes = 32 << 20

// The cap is measured in encoded JSON bytes. Larger documents can still be
// queried but are not retained globally after that operation.

type cachedDocument struct {
	mu     sync.Mutex
	member string
	value  any
}

func (d *DB) fullDocument(ctx context.Context, member string) (any, error) {
	cache := &d.documentCache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.member == member && cache.value != nil {
		return cache.value, nil
	}
	var packed []byte
	if err := d.db.QueryRowContext(ctx, "SELECT data FROM source_documents WHERE member=?", member).Scan(&packed); err != nil {
		return nil, d.documentError(member, err)
	}
	raw, err := inflateDocument(packed)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", member, err)
	}
	value, err := decodeStoredJSON(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", member, err)
	}
	if _, ok := value.(map[string]any); !ok {
		return nil, fmt.Errorf("catalog document %s is not an object", member)
	}
	if len(raw) <= maxCachedDocumentBytes {
		cache.member, cache.value = member, value
	}
	return value, nil
}

func (d *DB) documentPointerValue(ctx context.Context, document any, pointer string) (any, error) {
	parts, err := splitDocumentPointer(pointer)
	if err != nil {
		return nil, err
	}
	value := document
	for _, part := range parts {
		if value, err = d.resolveReferences(ctx, value, pointer); err != nil {
			return nil, err
		}
		if value, err = pointerChild(value, part, pointer); err != nil {
			return nil, err
		}
	}
	return value, nil
}

// resolveReferences restores the entity at one path step. Expanding every
// sibling's audit evidence before selecting a field would turn a narrow
// pointer lookup into a whole-document materialization.
func (d *DB) resolveReferences(ctx context.Context, value any, pointer string) (any, error) {
	active := make(referenceStack)
	for {
		node, ok := value.(map[string]any)
		if !ok {
			return value, nil
		}
		kind, ref, ok := storedReference(node)
		if !ok {
			return value, nil
		}
		id, err := referenceID(ref, kind)
		if err != nil {
			return nil, err
		}
		key := referenceKey{kind, id}
		if active[key] {
			return nil, fmt.Errorf("cyclic source reference at %q", pointer)
		}
		active[key] = true
		if value, err = d.restoreReference(ctx, kind, id); err != nil {
			return nil, err
		}
	}
}

func pointerChild(value any, part, pointer string) (any, error) {
	switch node := value.(type) {
	case map[string]any:
		child, ok := node[part]
		if !ok {
			return nil, fmt.Errorf("source pointer %q is missing", pointer)
		}
		return child, nil
	case []any:
		index, ok := pointerIndex(part)
		if !ok || index >= len(node) {
			return nil, fmt.Errorf("invalid source pointer %q", pointer)
		}
		return node[index], nil
	}
	return nil, fmt.Errorf("invalid source pointer %q", pointer)
}

// Every item refers to one authoritative source-document location.
func (d *DB) itemPayloadBytes(ctx context.Context, id int64) ([]byte, error) {
	var member, pointer string
	err := d.db.QueryRowContext(ctx, `SELECT d.member,p.pointer FROM item_occurrences o
JOIN source_documents d ON d.id=o.document_id
JOIN source_pointers p ON p.id=o.pointer_id WHERE o.id=?`, id).Scan(&member, &pointer)
	if err != nil {
		return nil, fmt.Errorf("item %d source: %w", id, err)
	}
	value, err := d.sourceValue(ctx, member, pointer)
	if err != nil {
		return nil, err
	}
	value, err = d.expandMetadata(ctx, value, make(referenceStack))
	if err != nil {
		return nil, err
	}
	return json.Marshal(value)
}
