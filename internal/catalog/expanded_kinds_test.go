package catalog

import (
	"bytes"
	"context"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

// Every stored record kind is queryable without a consumer allowlist.
func TestExpandedCatalogKinds(t *testing.T) {
	c := fixtureCatalog(t)
	ctx := context.Background()
	kinds := []string{"data-list", "generic-ffd-signal", "rob-code", "rob-signal", "command", "selector", "active-test-group", "customize-group", "customize-item", "customize-probe", "legacy-pid", "legacy-did", "legacy-freeze-frame", "legacy-active-test"}
	for n, kind := range kinds {
		c.Item(catalogtest.Item{ID: int64(100 + n), Category: 498, Kind: kind, Identifier: "sample", Name: "CoverageMarker", Detail: "partial",
			Payload: map[string]any{"name": "CoverageMarker", "semantic_status": "partial", "nested": map[string]any{"field": 7}}})
	}
	c.Exec(`UPDATE ecus SET counts='{"signals":1,"items_by_kind":{"rob-signal":1}}' WHERE region='NA' AND category_id=498`)
	d := openFixture(t, c)
	ecu, err := d.ECU(ctx, "NA", "frc")
	if err != nil {
		t.Fatal(err)
	}
	if ecu.Counts["signals"] != 1 || ecu.ItemsByKind["rob-signal"] != 1 {
		t.Fatalf("counts lost: %+v", ecu)
	}
	available, err := d.Kinds(ctx, "NA", 498)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, kind := range available {
		seen[kind] = true
	}
	for _, kind := range kinds {
		if !seen[kind] {
			t.Fatalf("kind %s not discoverable", kind)
		}
	}
	all, err := d.Search(ctx, "NA", "CoverageMarker", 100)
	if err != nil || len(all) != len(kinds) {
		t.Fatalf("search returned %d records, want %d: %v", len(all), len(kinds), err)
	}
	for _, kind := range kinds {
		t.Run(kind, func(t *testing.T) {
			matches, err := d.SearchKind(ctx, "NA", 498, kind, "CoverageMarker", 10)
			if err != nil || len(matches) != 1 {
				t.Fatalf("search: %#v, %v", matches, err)
			}
			rows, err := d.ECURecords(ctx, ecu, kind, 10)
			if err != nil || len(rows) != 1 {
				t.Fatalf("browse: %#v, %v", rows, err)
			}
			item, err := d.Item(ctx, rows[0].ID)
			if err != nil {
				t.Fatal(err)
			}
			if item.Kind != kind || !bytes.Contains(item.Payload, []byte(`"semantic_status":"partial"`)) || !bytes.Contains(item.Payload, []byte(`"nested":{"field":7}`)) {
				t.Fatalf("lost metadata: %+v", item)
			}
		})
	}
}
