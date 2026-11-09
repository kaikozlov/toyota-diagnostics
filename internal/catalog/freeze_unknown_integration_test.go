package catalog

import (
	"context"
	"testing"
)

func TestFreezeUnknownOptionalIntegration(t *testing.T) {
	d := productFreezeFixture(t)
	ctx := context.Background()
	before, e := d.legacyFreezeSignal(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := before.(map[string]any)["unassigned_fields"]; ok {
		t.Fatal("absent fields added")
	}
	d = productFreezeFixture(t)
	addUnknownFixture(t, d)
	after, e := d.legacyFreezeSignal(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	fields, ok := after.(map[string]any)["unassigned_fields"].(map[string]any)
	if !ok || len(fields) != 5 {
		t.Fatal(after)
	}
	d = productFreezeFixture(t)
	if _, e = d.db.Exec("UPDATE legacy_freeze_entries SET unknown_id=99"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.legacyFreezeSignal(ctx, 1); e == nil {
		t.Fatal("dangling unknown reference accepted")
	}
}
