package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func addUnknownFixture(t *testing.T, d *DB) {
	t.Helper()
	_, e := d.db.Exec(`INSERT INTO legacy_freeze_unknown_facts VALUES(1,X'0102030405060708090a0b0c',X'0d0e0f10',X'aabb',X'ccdd',255); UPDATE legacy_freeze_entries SET unknown_id=1`)
	if e != nil {
		t.Fatal(e)
	}
}
func TestFreezeUnknownFieldsExactAndFresh(t *testing.T) {
	d := productFreezeFixture(t)
	addUnknownFixture(t, d)
	ctx := context.Background()
	got, e := d.freezeUnknownFields(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if got["34_3f"] != "0102030405060708090a0b0c" || got["56_57"] != "aabb" || got["67"] != json.Number("255") {
		t.Fatal(got)
	}
	got["67"] = json.Number("0")
	again, e := d.freezeUnknownFields(ctx, 1)
	if e != nil || again["67"] != json.Number("255") {
		t.Fatal("mutation", e)
	}
}
func TestFreezeUnknownFieldsRejectCorruption(t *testing.T) {
	for _, q := range []string{"DELETE FROM legacy_freeze_unknown_facts", "UPDATE legacy_freeze_unknown_facts SET bytes_34_3f=X'00'", "UPDATE legacy_freeze_unknown_facts SET bytes_48_4b=X'00'", "UPDATE legacy_freeze_unknown_facts SET bytes_56_57=X'00'", "UPDATE legacy_freeze_unknown_facts SET bytes_5e_5f='abcd'", "UPDATE legacy_freeze_unknown_facts SET byte_67=256", "UPDATE legacy_freeze_unknown_facts SET byte_67=-1"} {
		d := productFreezeFixture(t)
		addUnknownFixture(t, d)
		if _, e := d.db.Exec(q); e != nil {
			t.Fatal(e)
		}
		if _, e := d.freezeUnknownFields(context.Background(), 1); e == nil {
			t.Fatal("corruption accepted", q)
		}
	}
}
