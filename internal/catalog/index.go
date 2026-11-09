package catalog

import (
	"context"
	"fmt"
	"strings"
)

// Index reads index.json for one region (empty means the catalog default),
// fully expanded. CAN topology is omitted; VehicleTopology reads one vehicle's.
func (d *DB) Index(ctx context.Context, region string) (map[string]any, error) {
	root, err := d.fullDocument(ctx, "index.json")
	if err != nil {
		return nil, err
	}
	index := root.(map[string]any)
	if region == "" {
		region, _ = index["default_region"].(string)
	}
	region = strings.ToUpper(region)
	regions, _ := index["regions"].(map[string]any)
	if _, ok := regions[region]; !ok {
		return nil, fmt.Errorf("catalog index has no region %q", region)
	}
	regional, err := d.sourceValue(ctx, "index.json", "/regions/"+escapePointer(region))
	if err != nil {
		return nil, err
	}
	object, ok := regional.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("catalog index region %s is not an object", region)
	}
	trimmed := make(map[string]any, len(object))
	for k, v := range object {
		if k != "can_topology" {
			trimmed[k] = v
		}
	}
	selected := make(map[string]any, len(index))
	for k, v := range index {
		if k != "regions" {
			selected[k] = v
		}
	}
	selected["regions"] = map[string]any{region: trimmed}
	value, err := d.runtimeValue(ctx, selected)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}
