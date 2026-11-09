package catalog

import (
	"bytes"
	"context"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"testing"
)

func operationFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	sql, e := os.ReadFile("testdata/product_active.sql")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = d.db.Exec(string(sql)); e != nil {
		t.Fatal(e)
	}
	return d
}
func TestOperationDefinitionsGoldenAndIsolation(t *testing.T) {
	d := operationFixture(t)
	data, e := os.ReadFile("testdata/product_active.json")
	if e != nil {
		t.Fatal(e)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var cases []struct {
		Kind  string
		ID    json.Number
		Value any
	}
	if e = dec.Decode(&cases); e != nil {
		t.Fatal(e)
	}
	for _, c := range cases {
		got, e := d.expandMetadata(context.Background(), map[string]any{c.Kind: c.ID}, make(referenceStack))
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(got, c.Value) {
			t.Fatalf("%s got %#v want %#v", c.Kind, got, c.Value)
		}
		got.(map[string]any)["mutated"] = true
		again, e := d.expandMetadata(context.Background(), map[string]any{c.Kind: c.ID}, make(referenceStack))
		if e != nil || !reflect.DeepEqual(again, c.Value) {
			t.Fatal("shared mutable cache", e)
		}
		runtime, e := d.runtimeValue(context.Background(), map[string]any{c.Kind: c.ID})
		if e != nil {
			t.Fatal(e)
		}
		if _, ok := runtime.(map[string]any); !ok {
			t.Fatal("semantic operation hidden")
		}
	}
}
func TestOperationDefinitionCorruption(t *testing.T) {
	for _, tc := range []struct{ change, kind string }{
		{"DELETE FROM operation_wire_frames", "$operation_frame"},
		{"UPDATE operation_wire_frames SET send='not a blob'", "$operation_frame"},
		{"UPDATE operation_encoding_policies SET p5_supported=2", "$operation_encoding"},
		{"DELETE FROM text_values WHERE id=(SELECT start_id FROM operation_mask_policies LIMIT 1)", "$operation_mask"},
		{"DELETE FROM text_values WHERE id=(SELECT frame_id FROM operation_base_frames LIMIT 1)", "$operation_base_frame"},
	} {
		t.Run(tc.change, func(t *testing.T) {
			d := operationFixture(t)
			if _, e := d.db.Exec(tc.change); e != nil {
				t.Fatal(e)
			}
			if _, e := d.operationDefinition(context.Background(), tc.kind, 1); e == nil {
				t.Fatal("corruption accepted")
			}
		})
	}
	d := operationFixture(t)
	for _, id := range []int64{0, -1, 999999} {
		if _, e := d.operationDefinition(context.Background(), "$operation_frame", id); e == nil {
			t.Fatal("missing ref accepted")
		}
	}
	if _, e := d.operationDefinition(context.Background(), "$not_operation", 1); e == nil {
		t.Fatal("unknown kind accepted")
	}
}
func TestOperationLargeIDsDoNotCollide(t *testing.T) {
	d := operationFixture(t)
	id := int64(math.MaxInt64)
	if _, e := d.db.Exec("INSERT INTO operation_mask_policies SELECT ?,start_id,stop_id,rule_id FROM operation_mask_policies WHERE id=1", id); e != nil {
		t.Fatal(e)
	}
	a, e := d.operationDefinition(context.Background(), "$operation_mask", 1)
	if e != nil {
		t.Fatal(e)
	}
	b, e := d.operationDefinition(context.Background(), "$operation_mask", id)
	if e != nil || !reflect.DeepEqual(a, b) {
		t.Fatal(b, e)
	}
}
