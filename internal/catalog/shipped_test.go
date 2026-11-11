package catalog

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

// openShipped opens catalog/catalog.sqlite. Only an undownloaded LFS pointer
// skips; any other failure is a broken checkout.
func openShipped(t *testing.T) *DB {
	t.Helper()
	d, err := Open("../../catalog/catalog.sqlite")
	if errors.Is(err, ErrLFSPointer) {
		t.Skip("catalog/catalog.sqlite is an LFS pointer; run git lfs pull")
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestShippedCatalogRuntimeContracts(t *testing.T) {
	d := openShipped(t)
	ctx := context.Background()
	index, err := d.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	regional := map[string]any{}
	for _, name := range []string{"routes", "session_control"} {
		if regional[name], err = d.RegionalSection(ctx, "NA", name); err != nil {
			t.Fatal(err)
		}
	}
	if table, _ := regional["session_control"].(map[string]any); len(table) == 0 {
		t.Error("NA index has no session_control")
	}
	if table, _ := index["support_contracts"].(map[string]any); len(table) == 0 {
		t.Error("index has no support_contracts")
	}
	route, _ := regional["routes"].(map[string]any)["498:18"].(map[string]any)
	if route["physical_request_address"] != float64(1938) || route["physical_response_address"] != float64(1946) {
		t.Errorf("route 498:18 = %v", route)
	}
}

// The fixture builder must create exactly the shipped schema.
func TestFixtureSchemaMatchesShippedCatalog(t *testing.T) {
	d := openShipped(t)
	rows, err := d.db.Query(`SELECT sql FROM sqlite_master WHERE sql IS NOT NULL
 AND name NOT LIKE 'sqlite_%' AND name NOT LIKE 'items_fts_%'
 ORDER BY CASE type WHEN 'table' THEN 0 WHEN 'index' THEN 1 WHEN 'view' THEN 2 ELSE 3 END, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var shipped []string
	for rows.Next() {
		var stmt string
		if err := rows.Scan(&stmt); err != nil {
			t.Fatal(err)
		}
		shipped = append(shipped, strings.TrimSpace(stmt)+";")
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	got := strings.Join(shipped, "\n")
	want := strings.TrimSpace(catalogtest.Schema[strings.Index(catalogtest.Schema, "\n")+1:])
	if got != want {
		t.Fatal("internal/catalog/catalogtest/schema.sql differs from catalog/catalog.sqlite; regenerate it from sqlite_master")
	}
}

// Entry and vehicle reads must agree with whole sections in every region.
func TestShippedRegionalEntriesMatchSections(t *testing.T) {
	d := openShipped(t)
	ctx := context.Background()
	for _, region := range []string{"NA", "EU", "JP"} {
		for _, name := range []string{"selected_categories", "routes", "install_sets", "vehicles"} {
			whole, err := d.RegionalSection(ctx, region, name)
			if err != nil {
				t.Fatal(err)
			}
			section := whole.(map[string]any)
			keys := make([]string, 0, len(section))
			for key := range section {
				keys = append(keys, key)
			}
			entries, err := d.RegionalEntries(ctx, region, name, append(keys, "absent"))
			if err != nil || !reflect.DeepEqual(entries, section) {
				t.Fatalf("%s %s entries differ from section: %v", region, name, err)
			}
			if name != "vehicles" {
				continue
			}
			vehicles, err := d.RegionalVehicles(ctx, region)
			if err != nil || len(vehicles) != len(section) {
				t.Fatalf("%s vehicles: %d of %d, %v", region, len(vehicles), len(section), err)
			}
			for key, row := range section {
				full := row.(map[string]any)
				want := map[string]any{"vehicle_type": full["vehicle_type"], "name": full["name"]}
				if !reflect.DeepEqual(vehicles[key], want) {
					t.Fatalf("%s vehicle %s = %v, want %v", region, key, vehicles[key], want)
				}
			}
			for key, row := range vehicles {
				vehicleType := int64(row.(map[string]any)["vehicle_type"].(float64))
				typed, err := d.RegionalVehicles(ctx, region, vehicleType)
				if err != nil || !reflect.DeepEqual(typed[key], row) {
					t.Fatalf("%s vehicles of type %d: %v %v", region, vehicleType, typed, err)
				}
				for _, other := range typed {
					if other.(map[string]any)["vehicle_type"] != float64(vehicleType) {
						t.Fatalf("%s vehicles of type %d include %v", region, vehicleType, other)
					}
				}
				break
			}
		}
	}
}
