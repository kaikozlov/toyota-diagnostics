package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func legacyOperationFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	sql, e := os.ReadFile("testdata/legacy_operation.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.db.Exec(string(sql)); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestLegacyOperationGolden(t *testing.T) {
	d := legacyOperationFixture(t)
	data, e := os.ReadFile("testdata/legacy_operation.json")
	if e != nil {
		t.Fatal(e)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var want any
	if e = dec.Decode(&want); e != nil {
		t.Fatal(e)
	}
	got, e := d.legacyOperation(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
	if _, e = d.legacyOperation(context.Background(), 999999); e == nil {
		t.Fatal("missing accepted")
	}
}
func TestLegacyOperationInvalidRelationships(t *testing.T) {
	for _, change := range []string{
		"DELETE FROM legacy_operation_steps WHERE position=(SELECT max(position) FROM legacy_operation_steps)",
		"UPDATE legacy_operation_steps SET position=position+1",
		"UPDATE legacy_operation_programs SET support_required=9",
		"DELETE FROM legacy_operation_pattern_entries",
		"DELETE FROM signal_infos",
		"DELETE FROM text_values WHERE id=(SELECT execution_id FROM legacy_operation_defs LIMIT 1)",
	} {
		t.Run(change, func(t *testing.T) {
			d := legacyOperationFixture(t)
			// Disable CHECK constraints to model a corrupted imported catalog.
			d.db.Exec("PRAGMA ignore_check_constraints=ON")
			if _, e := d.db.Exec(change); e != nil {
				t.Fatal(e)
			}
			if _, e := d.legacyOperation(context.Background(), 1); e == nil {
				t.Fatal("corruption accepted")
			}
		})
	}
}
func TestLegacyOperationNullNameAndExactInteger(t *testing.T) {
	d := legacyOperationFixture(t)
	if _, e := d.db.Exec("UPDATE legacy_operation_defs SET name_id=NULL,operation_id=9007199254740993"); e != nil {
		t.Fatal(e)
	}
	got, e := d.legacyOperation(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	r := got.(map[string]any)
	if r["name"] != nil || r["id"] != json.Number("9007199254740993") {
		t.Fatal(r)
	}
}
