package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

// partsFixture stores index.json with three semantic parts. mutate may corrupt
// the stored parts before the catalog is opened.
func partsFixture(t *testing.T, mutate func(c *catalogtest.Catalog)) *DB {
	t.Helper()
	c := catalogtest.New(t)
	c.Document("index.json", map[string]any{"default_region": "NA", "regions": map[string]any{"NA": map[string]any{
		"selected_categories": map[string]any{"1": map[string]any{"$part": 1}, "a/b": map[string]any{"$part": 2}},
		"can_topology":        map[string]any{"7": []any{map[string]any{"vehicle_type": 7}}},
		"unrelated":           map[string]any{"$part": 3}}}})
	c.Exec(`INSERT INTO source_pointers VALUES(1,'/regions/NA/selected_categories/1'),(2,'/regions/NA/selected_categories/a~1b'),(3,'/regions/NA/unrelated')`)
	for id, v := range map[int]any{
		1: map[string]any{"name": "target", "large": json.Number("18446744073709551615")},
		2: []any{map[string]any{"~key": "escaped"}},
		3: map[string]any{"other": "value"},
	} {
		c.Exec("INSERT INTO source_document_parts VALUES(1,?,?)", id, packedPart(t, v))
	}
	if mutate != nil {
		mutate(c)
	}
	return openFixture(t, c)
}

func TestDocumentPartsTargetingAndRoundTrip(t *testing.T) {
	d := partsFixture(t, nil)
	ctx := context.Background()
	got, err := d.sourceValue(ctx, "index.json", "/regions/NA/selected_categories/a~1b/0/~0key")
	if err != nil || got != "escaped" {
		t.Fatalf("%v %v", got, err)
	}
	selected, err := d.sourceValue(ctx, "index.json", "/regions/NA/selected_categories")
	if err != nil || len(selected.(map[string]any)) != 2 {
		t.Fatalf("%v %v", selected, err)
	}
	full, err := d.Document(ctx, "index.json")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(full)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "$part") {
		t.Fatalf("storage markers leaked: %s", encoded)
	}
	index, err := d.Index(ctx, "")
	if err != nil {
		t.Fatal(err)
	}
	regional := full["regions"].(map[string]any)["NA"].(map[string]any)
	delete(regional, "can_topology")
	if !reflect.DeepEqual(full, index) {
		t.Fatalf("regional index differs from full document:\n%v\n%v", index, full)
	}
	topology, err := d.VehicleTopology(ctx, "NA", 7)
	if err != nil || len(topology) != 1 {
		t.Fatalf("%v %v", topology, err)
	}
}

func TestDocumentPartsCorruptUnrelatedAndMissing(t *testing.T) {
	ctx := context.Background()
	d := partsFixture(t, func(c *catalogtest.Catalog) {
		c.Exec("UPDATE source_document_parts SET data=X'00' WHERE pointer_id=3")
	})
	got, err := d.sourceValue(ctx, "index.json", "/regions/NA/selected_categories/1/name")
	if err != nil || got != "target" {
		t.Fatalf("unrelated corrupt section affected target: %v %v", got, err)
	}
	if _, err = d.Document(ctx, "index.json"); err == nil {
		t.Fatal("corrupt full document accepted")
	}
	d = partsFixture(t, func(c *catalogtest.Catalog) {
		c.Exec("DELETE FROM source_document_parts WHERE pointer_id=1")
	})
	if _, err = d.sourceValue(ctx, "index.json", "/regions/NA/selected_categories/1"); err == nil || !strings.Contains(err.Error(), "missing document part") {
		t.Fatalf("missing part: %v", err)
	}
}

func TestDocumentPartIdentityMismatch(t *testing.T) {
	d := partsFixture(t, func(c *catalogtest.Catalog) {
		c.Document("index.json", map[string]any{"default_region": "NA", "regions": map[string]any{"NA": map[string]any{
			"selected_categories": map[string]any{"1": map[string]any{"$part": 999}}}}})
	})
	if _, err := d.sourceValue(context.Background(), "index.json", "/regions/NA/selected_categories/1"); err == nil || !strings.Contains(err.Error(), "identity mismatch") {
		t.Fatalf("%v", err)
	}
}
