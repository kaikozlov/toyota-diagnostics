package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func decodedBatchFixture(t *testing.T) *DB {
	t.Helper()
	s, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	s.SetMaxOpenConns(1)
	if _, e = s.Exec(decodedFixture); e != nil {
		s.Close()
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return &DB{db: s}
}
func TestDecodedMixedBatchOrderAndIsolation(t *testing.T) {
	d := decodedBatchFixture(t)
	ctx := context.Background()
	nodes := []any{}
	want := []any{}
	for i := 0; i < 600; i++ {
		name := decodedMonitorNames[i%len(decodedMonitorNames)]
		var id int64
		if e := d.db.QueryRow("SELECT min(id) FROM decoded_" + name).Scan(&id); e != nil {
			t.Fatal(e)
		}
		node := map[string]any{"$decoded_" + name: json.Number(fmt.Sprint(id))}
		value, e := d.runtimeValue(ctx, node)
		if e != nil {
			t.Fatal(e)
		}
		nodes = append(nodes, node)
		want = append(want, value)
		if i%19 == 0 {
			nodes = append(nodes, map[string]any{"future_semantic": json.Number("9")})
			want = append(want, map[string]any{"future_semantic": float64(9)})
		}
	}
	got, e := d.runtimeValue(ctx, nodes)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal("mixed batched output differs from point restoration")
	}
	values := got.([]any)
	values[0].(map[string]any)["key"] = "changed"
	if reflect.DeepEqual(values, want) {
		t.Fatal("mutation not applied")
	}
	again, e := d.runtimeValue(ctx, nodes)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(again, want) {
		t.Fatal("mutation escaped output into another restoration")
	}
	// Metadata output retains JSON numbers while runtime converts operational numbers.
	exact, e := d.expandMetadata(ctx, nodes, make(referenceStack))
	if e != nil {
		t.Fatal(e)
	}
	b, e := json.Marshal(exact)
	if e != nil {
		t.Fatal(e)
	}
	var normalized any
	json.Unmarshal(b, &normalized)
	if !reflect.DeepEqual(normalized, want) {
		t.Fatal("metadata batching differs")
	}
}
func TestDecodedBatchRejectsMissingRelationsAndCycles(t *testing.T) {
	for _, mode := range []string{"missing_child", "missing_definition", "cycle", "malformed"} {
		t.Run(mode, func(t *testing.T) {
			d := decodedBatchFixture(t)
			ctx := context.Background()
			marker := "$decoded_monitor"
			nodes := []any{}
			for i := 0; i < 4; i++ {
				nodes = append(nodes, map[string]any{marker: json.Number("1")})
			}
			active := make(referenceStack)
			switch mode {
			case "missing_child":
				if _, e := d.db.Exec("DELETE FROM decoded_monitor_pattern_records WHERE position=0"); e != nil {
					t.Fatal(e)
				}
			case "missing_definition":
				nodes[2] = map[string]any{marker: json.Number("9223372036854775807")}
			case "cycle":
				active[referenceKey{marker, 1}] = true
			case "malformed":
				nodes[2] = map[string]any{marker: json.Number("1"), "extra": true}
			}
			if _, handled, e := d.decodedMonitorArray(ctx, nodes, active); !handled || e == nil {
				t.Fatal("corruption accepted")
			}
		})
	}
}
