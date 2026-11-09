package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestLegacyOperationUnknownRanges(t *testing.T) {
	d := legacyOperationFixture(t)
	ctx := context.Background()
	textID := int64(900000)
	add := func(value string) int64 {
		textID++
		if _, e := d.db.Exec("INSERT OR IGNORE INTO text_values(id,value) VALUES(?,?)", textID, value); e != nil {
			t.Fatal(e)
		}
		var id int64
		if e := d.db.QueryRow("SELECT id FROM text_values WHERE value=?", value).Scan(&id); e != nil {
			t.Fatal(e)
		}
		return id
	}
	operation := []any{int64(1)}
	wanted := map[string]any{}
	for i, key := range legacyUnknownKeys["operation"] {
		value := strings.Repeat("ab", legacyUnknownLengths["operation"][i])
		operation = append(operation, add(value))
		wanted[key] = value
	}
	if _, e := d.db.Exec("INSERT INTO legacy_operation_unknown_facts VALUES(?,?,?,?,?,?,?,?)", operation...); e != nil {
		t.Fatal(e)
	}
	if _, e := d.db.Exec("INSERT INTO legacy_pattern_unknown_facts VALUES(1,?,?)", add("12345678"), add("abcd")); e != nil {
		t.Fatal(e)
	}
	if _, e := d.db.Exec("UPDATE legacy_operation_entries SET unknown_id=1; UPDATE legacy_operation_pattern_entries SET unknown_id=1"); e != nil {
		t.Fatal(e)
	}
	got, e := d.legacyOperation(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	unknown := got.(map[string]any)["uninterpreted_bytes"].(map[string]any)
	for key, want := range wanted {
		if unknown[key] != want {
			t.Fatal("lost unknown range", key)
		}
	}
	if got.(map[string]any)["pattern"].(map[string]any)["uninterpreted_bytes"].(map[string]any)["14_17"] != "12345678" {
		t.Fatal("lost pattern unknowns")
	}
	runtime, e := d.runtimeValue(ctx, map[string]any{"$legacy_operation_pattern": json.Number("1")})
	if e != nil {
		t.Fatal(e)
	}
	if runtime.(map[string]any)["uninterpreted_bytes"].(map[string]any)["22_23"] != "abcd" {
		t.Fatal("runtime lost pattern unknown")
	}
	if _, e := d.db.Exec("DELETE FROM legacy_pattern_unknown_facts"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.legacyOperationPattern(ctx, 1); e == nil {
		t.Fatal("missing unknown definition accepted")
	}
}
