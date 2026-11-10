package catalog

import (
	"context"
	"errors"
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
	index, err := d.Index(context.Background(), "NA")
	if err != nil {
		t.Fatal(err)
	}
	regional, _ := index["regions"].(map[string]any)["NA"].(map[string]any)
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

// Search relies on two facts about the catalog: every occurrence lies inside
// an owner range of its own region and ECU, and an ECU's own record carries
// the ECU's name.
func TestShippedCatalogSearchInvariants(t *testing.T) {
	d := openShipped(t)
	var outside, misnamed int
	if err := d.db.QueryRow(`SELECT count(*) FROM item_occurrences o WHERE NOT EXISTS (
  SELECT 1 FROM owner_ranges w WHERE w.region_id=o.region_id AND w.category_id=o.category_id
   AND o.id BETWEEN w.first_id AND w.last_id)`).Scan(&outside); err != nil {
		t.Fatal(err)
	}
	if err := d.db.QueryRow(`SELECT count(*) FROM item_occurrences o
JOIN search_records r ON r.id=o.record_id JOIN search_kinds k ON k.id=r.kind_id AND k.name='ecu'
JOIN search_regions g ON g.id=o.region_id JOIN text_values n ON n.id=r.name_id
LEFT JOIN ecus e ON e.region=g.name AND e.category_id=o.category_id
WHERE e.name IS NULL OR lower(e.name)!=lower(n.value)`).Scan(&misnamed); err != nil {
		t.Fatal(err)
	}
	if outside != 0 || misnamed != 0 {
		t.Fatalf("%d occurrences outside their owner ranges, %d ECU records not named after their ECU", outside, misnamed)
	}
}
