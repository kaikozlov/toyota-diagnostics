package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
)

type referenceKey struct {
	kind string
	id   int64
}
type referenceStack map[referenceKey]bool

func (d *DB) expandMetadata(ctx context.Context, value any, active referenceStack) (any, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch node := value.(type) {
	case map[string]any:
		if kind, ref, ok := storedReference(node); ok {
			id, err := referenceID(ref, kind)
			if err != nil {
				return nil, err
			}
			key := referenceKey{kind, id}
			if active[key] {
				return nil, fmt.Errorf("cyclic %s reference %d", kind, id)
			}
			record, err := d.restoreReference(ctx, kind, id)
			if err != nil {
				return nil, err
			}
			active[key] = true
			result, err := d.expandMetadata(ctx, record, active)
			delete(active, key)
			return result, err
		}
		if decoded, handled, err := d.vehicleDefinitionMap(ctx, node, active); handled || err != nil {
			return decoded, err
		}
		out := make(map[string]any, len(node))
		for key, child := range node {
			decoded, err := d.expandMetadata(ctx, child, active)
			if err != nil {
				return nil, err
			}
			out[key] = decoded
		}
		return out, nil
	case []any:
		if decoded, handled, err := d.nativeRecorderArray(ctx, node, active); handled || err != nil {
			if err != nil {
				return nil, err
			}
			return d.expandMetadata(ctx, decoded, active)
		}
		if decoded, handled, err := d.decodedMonitorArray(ctx, node, active); handled || err != nil {
			if err != nil {
				return nil, err
			}
			return d.expandMetadata(ctx, decoded, active)
		}
		if decoded, handled, err := d.dtcSourceArray(ctx, node, active); handled || err != nil {
			if err != nil {
				return nil, err
			}
			return d.expandMetadata(ctx, decoded, active)
		}
		if decoded, handled, err := d.customizeArray(ctx, node, active); handled || err != nil {
			if err != nil {
				return nil, err
			}
			return d.expandMetadata(ctx, decoded, active)
		}
		if decoded, handled, err := d.workStandardArray(ctx, node, active); handled || err != nil {
			if err != nil {
				return nil, err
			}
			return d.expandMetadata(ctx, decoded, active)
		}
		out := make([]any, len(node))
		for i, child := range node {
			decoded, err := d.expandMetadata(ctx, child, active)
			if err != nil {
				return nil, err
			}
			out[i] = decoded
		}
		return out, nil
	default:
		return value, nil
	}
}

// runtimeJSONValue converts the newly expanded (never cached) tree to the
// runtime API's existing float64 numeric representation without serializing a
// whole large document a second time.
func runtimeJSONValue(value any) (any, error) {
	switch node := value.(type) {
	case json.Number:
		return node.Float64()
	case map[string]any:
		for key, child := range node {
			converted, err := runtimeJSONValue(child)
			if err != nil {
				return nil, err
			}
			node[key] = converted
		}
	case []any:
		for i, child := range node {
			converted, err := runtimeJSONValue(child)
			if err != nil {
				return nil, err
			}
			node[i] = converted
		}
	}
	return value, nil
}

// Retain only small reusable provenance nodes. Large evidence lists and a walk
// through the entire corpus must not turn this cache into a second database.
type cachedMetadata struct {
	mu     sync.Mutex
	values map[int64]metadataEntry
	order  []int64
	bytes  int
}
type metadataEntry struct {
	value any
	bytes int
}

func (c *cachedMetadata) load(id int64) (any, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	entry, ok := c.values[id]
	return entry.value, ok
}

func (c *cachedMetadata) store(id int64, value any, size int) {
	if size > 64<<10 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.values == nil {
		c.values = make(map[int64]metadataEntry)
	}
	if _, ok := c.values[id]; ok {
		return
	}
	for len(c.order) >= 256 || c.bytes+size > 4<<20 {
		first := c.order[0]
		c.order = c.order[1:]
		c.bytes -= c.values[first].bytes
		delete(c.values, first)
	}
	c.values[id] = metadataEntry{value, size}
	c.order = append(c.order, id)
	c.bytes += size
}
