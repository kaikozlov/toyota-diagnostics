package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRuntimeProductSemanticsAreEager(t *testing.T) {
	d := emptyDB(t)
	ctx := context.Background()
	value, err := d.runtimeValue(ctx, map[string]any{"selectors": []any{map[string]any{"selector": json.Number("202")}}, "work_factor": map[string]any{"name": "Useful diagnostic fact"}})
	if err != nil {
		t.Fatal(err)
	}
	fields := value.(map[string]any)
	if fields["selectors"].([]any)[0].(map[string]any)["selector"] != float64(202) {
		t.Fatal(fields)
	}
	if fields["work_factor"].(map[string]any)["name"] != "Useful diagnostic fact" {
		t.Fatal(fields)
	}
}
func TestMalformedReservedReferenceCannotLeak(t *testing.T) {
	d := emptyDB(t)
	ctx := context.Background()
	node := map[string]any{"$signal_info": json.Number("1"), "unexpected": "field"}
	if _, err := d.expandMetadata(ctx, node, make(referenceStack)); err == nil {
		t.Fatal("expanded malformed marker")
	}
	if _, err := d.runtimeValue(ctx, node); err == nil {
		t.Fatal("runtime leaked malformed marker")
	}
	if _, err := d.documentPointerValue(ctx, node, "/unexpected"); err == nil {
		t.Fatal("pointer bypassed malformed marker")
	}
}
