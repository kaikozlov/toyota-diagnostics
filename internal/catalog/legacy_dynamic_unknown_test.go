package catalog

import (
	"context"
	"testing"
)

func TestLegacyDynamicUnknownRanges(t *testing.T) {
	d := legacyConditionFixture(t)
	ctx := context.Background()
	_, e := d.db.Exec("INSERT INTO text_values VALUES(2,'abcdef'),(3,''); INSERT INTO legacy_dynamic_unknown_facts VALUES(1,2,3); UPDATE legacy_dynamic_entries SET unknown_id=1")
	if e != nil {
		t.Fatal(e)
	}
	value, e := d.legacyDynamic(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	fields := value.(map[string]any)["uninterpreted_bytes"].(map[string]any)
	if fields["14_16"] != "abcdef" || fields["18_end"] != "" {
		t.Fatal(fields)
	}
	for _, q := range []string{"UPDATE legacy_dynamic_entries SET unknown_id=9", "DELETE FROM text_values WHERE id=2"} {
		d = legacyConditionFixture(t)
		_, e = d.db.Exec("INSERT INTO text_values VALUES(2,'abcdef'),(3,''); INSERT INTO legacy_dynamic_unknown_facts VALUES(1,2,3); UPDATE legacy_dynamic_entries SET unknown_id=1;" + q)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = d.legacyDynamic(ctx, 1); e == nil {
			t.Fatal("corruption accepted")
		}
	}
}
