package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func TestWorkUnknownBytesPointAndBatch(t *testing.T) {
	d := workSemanticFixture(t)
	if _, e := d.db.Exec("UPDATE work_standard_definitions SET unknown_bytes=x'ff';UPDATE work_help_definitions SET unknown_bytes=x'12ff'"); e != nil {
		t.Fatal(e)
	}
	value, e := d.workStandard(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	row := value.(map[string]any)
	if row["uninterpreted_bytes"].(map[string]any)["11"] != "ff" {
		t.Fatal(row)
	}
	help := row["standard_value_help_records"].([]any)[0].(map[string]any)
	if help["uninterpreted_bytes"].(map[string]any)["10"] != "12ff" {
		t.Fatal(help)
	}
	nodes := make([]any, 40)
	for i := range nodes {
		nodes[i] = map[string]any{"$work_standard": json.Number("1")}
	}
	decoded, e := d.expandMetadata(context.Background(), nodes, referenceStack{})
	if e != nil {
		t.Fatal(e)
	}
	first := decoded.([]any)[0].(map[string]any)
	second := decoded.([]any)[1].(map[string]any)
	first["uninterpreted_bytes"].(map[string]any)["11"] = "changed"
	if second["uninterpreted_bytes"].(map[string]any)["11"] != "ff" {
		t.Fatal("unknown aliases")
	}
	for _, field := range []workSemanticField{{"uninterpreted_bytes", "unknown_bytes", '1'}, {"uninterpreted_bytes", "unknown_bytes", '2'}, {"uninterpreted_bytes", "unknown_bytes", '3'}, {"uninterpreted_bytes", "unknown_bytes", '4'}} {
		if _, e := workDecodedFields([]workSemanticField{field}, []any{[]byte{}}); e == nil {
			t.Fatal("corrupt width accepted")
		}
	}
}
