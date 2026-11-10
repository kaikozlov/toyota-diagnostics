package catalog

import (
	"context"
	"fmt"
	"strings"
)

// Index reads index.json without its regional tables, fully expanded.
// RegionalSection and RegionalEntries read those one at a time.
func (d *DB) Index(ctx context.Context) (map[string]any, error) {
	root, err := d.fullDocument(ctx, "index.json")
	if err != nil {
		return nil, err
	}
	index := root.(map[string]any)
	selected := make(map[string]any, len(index))
	for k, v := range index {
		if k != "regions" {
			selected[k] = v
		}
	}
	value, err := d.runtimeValue(ctx, selected)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

// regionalPointer locates one section of a region's index and reports
// whether the region has it.
func (d *DB) regionalPointer(ctx context.Context, region, section string) (string, bool, error) {
	root, err := d.fullDocument(ctx, "index.json")
	if err != nil {
		return "", false, err
	}
	region = strings.ToUpper(region)
	regions, _ := root.(map[string]any)["regions"].(map[string]any)
	keys, ok := regions[region].(map[string]any)
	if !ok {
		return "", false, fmt.Errorf("catalog index has no region %q", region)
	}
	_, ok = keys[section]
	return "/regions/" + escapePointer(region) + "/" + escapePointer(section), ok, nil
}

// RegionalSection reads one section of a region's index, fully expanded. It
// is nil when the region has no such section.
func (d *DB) RegionalSection(ctx context.Context, region, section string) (any, error) {
	pointer, ok, err := d.regionalPointer(ctx, region, section)
	if err != nil || !ok {
		return nil, err
	}
	value, err := d.sourceValue(ctx, "index.json", pointer)
	if err != nil {
		return nil, err
	}
	return d.runtimeValue(ctx, value)
}

// RegionalEntries reads the named entries of one regional section, fully
// expanded and keyed as in the index. Absent entries are omitted; the result
// is nil when the region has no such section.
func (d *DB) RegionalEntries(ctx context.Context, region, section string, keys []string) (map[string]any, error) {
	stored, err := d.storedEntries(ctx, region, section, keys)
	if err != nil || stored == nil {
		return nil, err
	}
	value, err := d.runtimeValue(ctx, stored)
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}

// storedEntries selects entries before anything is expanded. A section
// stored as one part is decoded once; otherwise each entry is read alone.
func (d *DB) storedEntries(ctx context.Context, region, section string, keys []string) (map[string]any, error) {
	pointer, ok, err := d.regionalPointer(ctx, region, section)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, nil
	}
	selected := make(map[string]any, len(keys))
	if keys == nil {
		return selected, nil
	}
	stored, found, err := d.containingPart(ctx, "index.json", pointer, keys)
	if err != nil {
		return nil, err
	}
	if found {
		return stored.(map[string]any), nil
	}
	// The section is not one part; its entries may be.
	root, err := d.fullDocument(ctx, "index.json")
	if err != nil {
		return nil, err
	}
	value, err := d.documentPointerValue(ctx, root, pointer)
	if err != nil {
		return nil, err
	}
	entries, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("catalog index %s is not an object", section)
	}
	// Entries stored as their own parts are read with one query.
	parts := map[string]string{}
	args := []any{"index.json"}
	for _, key := range keys {
		entry, ok := entries[key]
		if !ok {
			continue
		}
		if node, ok := entry.(map[string]any); ok && len(node) == 1 && node["$part"] != nil {
			child := pointer + "/" + escapePointer(key)
			if _, seen := parts[child]; !seen {
				parts[child] = key
				args = append(args, child)
			}
			continue
		}
		if selected[key], err = d.sourceValue(ctx, "index.json", pointer+"/"+escapePointer(key)); err != nil {
			return nil, err
		}
	}
	if len(parts) == 0 {
		return selected, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT p.id,p.pointer,r.data FROM source_documents d
JOIN source_document_parts r ON r.document_id=d.id JOIN source_pointers p ON p.id=r.pointer_id
WHERE d.member=? AND p.pointer IN (`+strings.Repeat(",?", len(parts))[1:]+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var part string
		var packed []byte
		if err := rows.Scan(&id, &part, &packed); err != nil {
			return nil, err
		}
		key := parts[part]
		if err := validatePartMarker(entries[key], id, part); err != nil {
			return nil, err
		}
		value, err := storedJSON(packed)
		if err != nil {
			return nil, fmt.Errorf("index.json%s: %w", part, err)
		}
		if err := unresolvedParts(value); err != nil {
			return nil, err
		}
		selected[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, key := range parts {
		if _, ok := selected[key]; !ok {
			return nil, fmt.Errorf("index.json%s/%s: %w", pointer, escapePointer(key), unresolvedParts(entries[key]))
		}
	}
	return selected, nil
}

// RegionalVehicles reads a region's vehicles keyed as in the index, each with
// only its vehicle_type and name. Given vehicle types, it may leave out
// vehicles of other types. RegionalEntries reads a chosen vehicle with its
// install sets.
func (d *DB) RegionalVehicles(ctx context.Context, region string, types ...int64) (map[string]any, error) {
	pointer, ok, err := d.regionalPointer(ctx, region, "vehicles")
	if err != nil || !ok {
		return nil, err
	}
	stored, err := d.sourceValue(ctx, "index.json", pointer)
	if err != nil {
		return nil, err
	}
	entries, ok := stored.(map[string]any)
	if !ok {
		return nil, nil
	}
	refs := make(map[string]int64, len(entries))
	ids := make([]int64, 0, len(entries))
	inline := make(map[string]any)
	for key, entry := range entries {
		if node, ok := entry.(map[string]any); ok {
			if kind, ref, ok := storedReference(node); ok && kind == "$vehicle_definition" {
				id, err := referenceID(ref, kind)
				if err != nil {
					return nil, err
				}
				refs[key] = id
				ids = append(ids, id)
				continue
			}
		}
		inline[key] = entry
	}
	names, err := d.vehicleNames(ctx, ids, types)
	if err != nil {
		return nil, err
	}
	value, err := d.runtimeValue(ctx, inline)
	if err != nil {
		return nil, err
	}
	vehicles := value.(map[string]any)
	for key, entry := range vehicles {
		if row, ok := entry.(map[string]any); ok {
			named := map[string]any{}
			for _, k := range []string{"vehicle_type", "name"} {
				if v, ok := row[k]; ok {
					named[k] = v
				}
			}
			vehicles[key] = named
		}
	}
	for key, id := range refs {
		if row, ok := names[id]; ok {
			vehicles[key] = map[string]any{"vehicle_type": row.vehicleType, "name": row.name}
		}
	}
	return vehicles, nil
}

func escapePointer(token string) string {
	return strings.NewReplacer("~", "~0", "/", "~1").Replace(token)
}
