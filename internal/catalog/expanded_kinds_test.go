package catalog

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

// New exported capabilities must remain queryable without a consumer allowlist.
func TestExpandedCatalogKinds(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	path := d.Path
	if err := d.Close(); err != nil {
		t.Fatal(err)
	}
	writable, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer writable.Close()
	kinds := []string{"data-list", "generic-ffd-signal", "rob-code", "rob-signal", "command", "selector", "active-test-group", "customize-group", "customize-item", "customize-probe", "legacy-pid", "legacy-did", "legacy-freeze-frame", "legacy-active-test"}
	for n, kind := range kinds {
		id := int64(100 + n)
		payload, err := json.Marshal(map[string]any{"name": "CoverageMarker", "semantic_status": "partial", "nested": map[string]any{"field": 7}})
		if err != nil {
			t.Fatal(err)
		}
		var compressed bytes.Buffer
		writer := zlib.NewWriter(&compressed)
		if _, err := writer.Write(payload); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := writable.Exec("INSERT INTO items VALUES(?, 'NA', 498, ?, 'sample', 'CoverageMarker', 'partial', ?)", id, kind, string(payload)); err != nil {
			t.Fatal(err)
		}
		if _, err := writable.Exec("INSERT INTO item_payloads VALUES(?, ?)", id, compressed.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := writable.Exec(`UPDATE ecus SET counts='{"signals":1,"items_by_kind":{"rob-signal":1}}' WHERE region='NA' AND category_id=498`); err != nil {
		t.Fatal(err)
	}
	if _, err := writable.Exec("INSERT INTO items_fts(items_fts) VALUES('rebuild')"); err != nil {
		t.Fatal(err)
	}
	if err := writable.Close(); err != nil {
		t.Fatal(err)
	}
	d, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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
