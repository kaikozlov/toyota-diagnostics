package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

// ecuCatalog restores one normalized configuration and its owner metadata.
// Nested semantic references stay unexpanded until the requested pointer is
// selected. Only one configuration body is cached, with the document byte cap.
func (d *DB) ecuCatalog(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid ECU catalog reference")
	}
	var definition int64
	var packedCategory []byte
	if err := d.db.QueryRowContext(ctx, "SELECT definition_id,category FROM ecu_catalog_bindings WHERE document_id=?", id).Scan(&definition, &packedCategory); err != nil {
		return nil, fmt.Errorf("ECU catalog binding %d: %w", id, err)
	}
	if definition <= 0 {
		return nil, fmt.Errorf("invalid ECU catalog definition reference")
	}
	category, err := storedJSON(packedCategory)
	if err != nil {
		return nil, fmt.Errorf("ECU catalog category: %w", err)
	}
	owner, ok := category.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid ECU catalog category")
	}
	cache := &d.ecuConfigurationCache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	key := strconv.FormatInt(definition, 10)
	var body map[string]any
	if cache.member == key && cache.value != nil {
		body = cache.value.(map[string]any)
	} else {
		var packed []byte
		if err := d.db.QueryRowContext(ctx, "SELECT data FROM ecu_catalog_definitions WHERE id=?", definition).Scan(&packed); err != nil {
			return nil, fmt.Errorf("ECU catalog definition %d: %w", definition, err)
		}
		raw, err := inflateDocument(packed)
		if err != nil {
			return nil, err
		}
		var decoded any
		// storedJSON preserves integer spelling; retaining normalized refs avoids
		// executing queries for unrelated catalog domains during item inspection.
		decoder := json.NewDecoder(bytes.NewReader(raw))
		decoder.UseNumber()
		if err = decoder.Decode(&decoded); err != nil {
			return nil, err
		}
		body, ok = decoded.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid ECU catalog body")
		}
		if _, exists := body["category"]; exists {
			return nil, fmt.Errorf("ECU catalog body duplicates owner category")
		}
		if _, exists := body["$ecu_catalog"]; exists {
			return nil, fmt.Errorf("recursive ECU catalog definition")
		}
		if len(raw) <= maxCachedDocumentBytes {
			cache.member, cache.value = key, body
		}
	}
	result := make(map[string]any, len(body)+1)
	for k, v := range body {
		result[k] = v
	}
	result["category"] = owner
	return result, nil
}
