package catalog

import (
	"context"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

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

func TestSearchScopeEndingAtZeroStaysFound(t *testing.T) {
	c := catalogtest.New(t)
	c.ECU(catalogtest.ECU{Category: 7, Key: "abs", Name: "Brakes", Database: "ABS"})
	c.Item(catalogtest.Item{ID: -1, Category: 7, Kind: "did", Identifier: "0x01", Name: "Wheel speed"},
		catalogtest.Item{ID: 0, Category: 7, Kind: "did", Identifier: "0x02", Name: "Wheel slip"})
	d := openFixture(t, c)
	for range 2 {
		rows, err := d.SearchKind(context.Background(), "NA", 7, "", "wheel", 10)
		if err != nil || len(rows) != 2 {
			t.Fatalf("%v %v", identifiers(rows), err)
		}
	}
}
