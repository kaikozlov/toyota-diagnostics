package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func TestLegacyOperationPatternRootSharesFacts(t *testing.T) {
	d := legacyOperationFixture(t)
	ctx := context.Background()
	var id int64
	if e := d.db.QueryRow("SELECT id FROM legacy_operation_pattern_entries LIMIT 1").Scan(&id); e != nil {
		t.Fatal(e)
	}
	root, e := d.legacyOperationPatternView(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if root.(map[string]any)["kind"] != "active_test_pattern" {
		t.Fatal(root)
	}
	nested, e := d.legacyOperationPattern(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := nested["kind"]; ok {
		t.Fatal("root kind leaked into operation child")
	}
	root.(map[string]any)["source"] = "mutated"
	again, e := d.legacyOperationPatternView(ctx, id)
	if e != nil || again.(map[string]any)["source"] == "mutated" {
		t.Fatal("mutation leaked", e)
	}
	runtime, e := d.runtimeValue(ctx, map[string]any{"$legacy_operation_pattern": json.Number("1")})
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := runtime.(map[string]any)["table"].(float64); !ok {
		t.Fatal("runtime number type")
	}
	if _, e := d.legacyOperationPatternView(ctx, 999999); e == nil {
		t.Fatal("missing accepted")
	}
	if _, e := d.db.Exec("DELETE FROM legacy_operation_patterns"); e != nil {
		t.Fatal(e)
	}
	if _, e := d.legacyOperationPatternView(ctx, id); e == nil {
		t.Fatal("missing facts accepted")
	}
}
