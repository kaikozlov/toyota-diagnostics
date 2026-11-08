package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Index rebuilds the recovered runtime index from the public sqlite-v2
// exporter tables. tools/gts bundle sqlite never stores index.json as a
// document; vehicles, ECU category rows, install-set routes, customize
// membership, and utility-family bindings are projected into structured
// tables and item payloads instead.
//
// Session control, support contracts, commsets, and CAN topology are not
// copied by that exporter. When a source bundle is attached, its index
// is used so those fields remain available.
func (d *DB) Index(ctx context.Context) (map[string]any, error) {
	if d.bundle != nil {
		index, err := d.bundle.Index()
		if err != nil {
			return nil, err
		}
		return normalizeIndex(index), nil
	}
	return d.reconstructIndex(ctx)
}

func normalizeIndex(index map[string]any) map[string]any {
	if index == nil {
		return map[string]any{}
	}
	regions, _ := index["regions"].(map[string]any)
	for _, raw := range regions {
		regional, _ := raw.(map[string]any)
		if regional == nil {
			continue
		}
		if regional["selected_categories"] == nil && regional["categories"] != nil {
			regional["selected_categories"] = regional["categories"]
		}
	}
	return index
}

func (d *DB) reconstructIndex(ctx context.Context) (map[string]any, error) {
	index := map[string]any{"regions": map[string]any{}}
	if v, err := d.Meta(ctx, "default_region"); err == nil && v != "" {
		index["default_region"] = v
	} else {
		index["default_region"] = "NA"
	}
	if v, err := d.Meta(ctx, "release"); err == nil && v != "" {
		index["release"] = v
	}
	if v, err := d.Meta(ctx, "source_schema"); err == nil && v != "" {
		index["schema"] = v
	}
	index["projection"] = schema

	regions := index["regions"].(map[string]any)
	regionOf := func(name string) map[string]any {
		name = strings.ToUpper(name)
		row, _ := regions[name].(map[string]any)
		if row == nil {
			row = map[string]any{
				"region":              name,
				"selected_categories": map[string]any{},
				"vehicles":            map[string]any{},
				"install_sets":        map[string]any{},
				"routes":              map[string]any{},
				"utilities":           map[string]any{"bindings": []any{}},
			}
			regions[name] = row
		}
		return row
	}

	if err := d.projectECUCategories(ctx, regionOf); err != nil {
		return nil, err
	}
	if err := d.projectVehicles(ctx, regionOf); err != nil {
		return nil, err
	}
	if err := d.projectInstallRoutes(ctx, regionOf); err != nil {
		return nil, err
	}
	if err := d.projectUtilityBindings(ctx, regionOf); err != nil {
		return nil, err
	}
	if err := d.projectCustomizeMembers(ctx, regionOf); err != nil {
		return nil, err
	}

	if len(regions) == 0 {
		return nil, d.missingRuntime("index.json", "this catalog is browse-only (no ECU or vehicle projection); search may still work, but live commands, recovered plans, and document decode need a catalog generated with `tools/gts bundle sqlite`")
	}
	haveCategories := false
	for _, raw := range regions {
		regional, _ := raw.(map[string]any)
		cats, _ := regional["selected_categories"].(map[string]any)
		if len(cats) > 0 {
			haveCategories = true
			break
		}
	}
	if !haveCategories {
		return nil, d.missingRuntime("index.json", "this catalog is browse-only (no ECU catalog projection); search may still work, but live commands, recovered plans, and document decode need a catalog generated with `tools/gts bundle sqlite`")
	}
	return index, nil
}

func (d *DB) projectECUCategories(ctx context.Context, regionOf func(string) map[string]any) error {
	// Exporter stores the full selected_categories row as an ecu item payload
	// without an item_sources pointer. Fall back to the compact ecus table.
	if ok, err := d.hasTable(ctx, "items"); err != nil {
		return err
	} else if ok {
		if err := d.forKindPayloads(ctx, "ecu", func(region string, categoryID int, payload any) error {
			row, _ := payload.(map[string]any)
			if row == nil {
				return nil
			}
			regional := regionOf(region)
			cats := regional["selected_categories"].(map[string]any)
			key := strconv.Itoa(categoryID)
			if raw, _ := row["category_id"]; raw != nil && strconv.Itoa(asIndexInt(raw)) != "0" {
				key = strconv.Itoa(asIndexInt(raw))
			}
			cats[key] = row
			return nil
		}); err != nil {
			return err
		}
	}
	ok, err := d.hasTable(ctx, "ecus")
	if err != nil || !ok {
		return err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT region, category_id, coalesce(key,''), coalesce(name,''), coalesce(database_name,''), generation, coalesce(support_mode,''), coalesce(catalog_member,'') FROM ecus`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var region, key, name, database, support, member string
		var categoryID int
		var generation sql.NullInt64
		if err := rows.Scan(&region, &categoryID, &key, &name, &database, &generation, &support, &member); err != nil {
			return err
		}
		regional := regionOf(region)
		cats := regional["selected_categories"].(map[string]any)
		id := strconv.Itoa(categoryID)
		if existing, _ := cats[id].(map[string]any); existing != nil {
			continue
		}
		row := map[string]any{
			"category_id":    categoryID,
			"name":           name,
			"short_name":     key,
			"database":       database,
			"support_mode":   support,
			"catalog_member": member,
		}
		if generation.Valid {
			row["generation"] = int(generation.Int64)
		}
		cats[id] = row
	}
	return rows.Err()
}

func (d *DB) projectVehicles(ctx context.Context, regionOf func(string) map[string]any) error {
	if ok, err := d.hasTable(ctx, "items"); err != nil {
		return err
	} else if ok {
		if err := d.forKindPayloads(ctx, "vehicle", func(region string, _ int, payload any) error {
			row, _ := payload.(map[string]any)
			if row == nil {
				return nil
			}
			regional := regionOf(region)
			vehicles := regional["vehicles"].(map[string]any)
			key := strconv.Itoa(asIndexInt(row["vehicle_type"]))
			if key == "0" && row["vehicle_type"] == nil {
				return nil
			}
			vehicles[key] = row
			return nil
		}); err != nil {
			return err
		}
	}
	ok, err := d.hasTable(ctx, "vehicles")
	if err != nil || !ok {
		return err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT region, vehicle_type, name, install_set_ids FROM vehicles`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var region, name, sets string
		var vehicleType int
		if err := rows.Scan(&region, &vehicleType, &name, &sets); err != nil {
			return err
		}
		regional := regionOf(region)
		vehicles := regional["vehicles"].(map[string]any)
		key := strconv.Itoa(vehicleType)
		if _, exists := vehicles[key]; exists {
			continue
		}
		var ids any
		if sets != "" {
			if err := json.Unmarshal([]byte(sets), &ids); err != nil {
				return fmt.Errorf("vehicle %s/%d install_set_ids: %w", region, vehicleType, err)
			}
		}
		vehicles[key] = map[string]any{"vehicle_type": vehicleType, "name": name, "install_set_ids": ids}
	}
	return rows.Err()
}

func (d *DB) projectInstallRoutes(ctx context.Context, regionOf func(string) map[string]any) error {
	ok, err := d.hasTable(ctx, "vehicle_ecus")
	if err != nil || !ok {
		return err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT region, install_set_id, category_id, route_selector, connection_frame_id, route_key, request_address, address_extension, transport_kind FROM vehicle_ecus`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var region, routeKey, transport string
		var setID, categoryID, selector, frame int
		var request, extension sql.NullInt64
		if err := rows.Scan(&region, &setID, &categoryID, &selector, &frame, &routeKey, &request, &extension, &transport); err != nil {
			return err
		}
		regional := regionOf(region)
		sets := regional["install_sets"].(map[string]any)
		key := strconv.Itoa(setID)
		list, _ := sets[key].([]any)
		list = append(list, map[string]any{
			"category_id":               categoryID,
			"connection_route_selector": selector,
			"connection_frame_id":       frame,
			"route_key":                 routeKey,
		})
		sets[key] = list
		if routeKey == "" {
			continue
		}
		routes := regional["routes"].(map[string]any)
		if _, exists := routes[routeKey]; exists {
			continue
		}
		route := map[string]any{"route_key": routeKey, "transport_kind": transport}
		if request.Valid {
			route["physical_request_address"] = int(request.Int64)
			route["request_address"] = int(request.Int64)
		}
		if extension.Valid {
			route["address_extension"] = int(extension.Int64)
		}
		routes[routeKey] = route
	}
	return rows.Err()
}

func (d *DB) projectUtilityBindings(ctx context.Context, regionOf func(string) map[string]any) error {
	ok, err := d.hasTable(ctx, "items")
	if err != nil || !ok {
		return err
	}
	return d.forKindPayloads(ctx, "utility-family", func(region string, _ int, payload any) error {
		row, _ := payload.(map[string]any)
		if row == nil {
			return nil
		}
		regional := regionOf(region)
		utilities, _ := regional["utilities"].(map[string]any)
		if utilities == nil {
			utilities = map[string]any{}
			regional["utilities"] = utilities
		}
		list, _ := utilities["bindings"].([]any)
		utilities["bindings"] = append(list, row)
		return nil
	})
}

func (d *DB) projectCustomizeMembers(ctx context.Context, regionOf func(string) map[string]any) error {
	ok, err := d.hasTable(ctx, "catalog_sources")
	if err != nil || !ok {
		return err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT region, member FROM catalog_sources WHERE category_id=0`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var region, member string
		if err := rows.Scan(&region, &member); err != nil {
			return err
		}
		if !strings.Contains(member, "customize/") {
			continue
		}
		regionOf(region)["customize_member"] = member
	}
	return rows.Err()
}

func (d *DB) forKindPayloads(ctx context.Context, kind string, fn func(region string, categoryID int, payload any) error) error {
	ok, err := d.hasTable(ctx, "item_payloads")
	if err != nil || !ok {
		return err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT i.region, i.category_id, p.data FROM items i JOIN item_payloads p ON p.item_id=i.id WHERE i.kind=? ORDER BY i.id`, kind)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var region string
		var categoryID int
		var data []byte
		if err := rows.Scan(&region, &categoryID, &data); err != nil {
			return err
		}
		raw, err := inflateDocument(data)
		if err != nil {
			return err
		}
		var payload any
		if err := json.Unmarshal(raw, &payload); err != nil {
			return err
		}
		if err := fn(region, categoryID, payload); err != nil {
			return err
		}
	}
	return rows.Err()
}

func asIndexInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(n)
		return i
	}
	return 0
}
