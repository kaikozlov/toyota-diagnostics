package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func TestWorkBatchOrderDuplicatesAndIsolation(t *testing.T) {
	d := workSemanticFixture(t)
	_, err := d.db.Exec("INSERT INTO work_standard_definitions SELECT 2,exception_handler_flag,exception_handler_id,help_id,record,123,source_id,standard_value_id_text,standard_value_id,standard_value_status_id,table_type,NULL,NULL FROM work_standard_definitions WHERE id=1")
	if err != nil {
		t.Fatal(err)
	}
	nodes := make([]any, 300)
	for i := range nodes {
		id := json.Number("1")
		if i%3 == 0 {
			id = "2"
		}
		nodes[i] = map[string]any{"$work_standard": id}
	}
	result, err := d.expandMetadata(context.Background(), nodes, make(referenceStack))
	if err != nil {
		t.Fatal(err)
	}
	rows := result.([]any)
	for i, row := range rows {
		want := json.Number("9007199254740993")
		if i%3 == 0 {
			want = "123"
		}
		if row.(map[string]any)["signal_id"] != want {
			t.Fatal(i, row)
		}
	}
	rows[1].(map[string]any)["signal_id"] = "changed"
	if rows[2].(map[string]any)["signal_id"] != json.Number("9007199254740993") {
		t.Fatal("duplicate alias")
	}
	rows[1].(map[string]any)["standard_value_help_records"].([]any)[0].(map[string]any)["text"] = "changed"
	if rows[2].(map[string]any)["standard_value_help_records"].([]any)[0].(map[string]any)["text"] != "" {
		t.Fatal("nested help alias")
	}
	runtime, err := d.runtimeValue(context.Background(), nodes)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.([]any)) != 300 {
		t.Fatal("runtime rows lost")
	}
}
func TestWorkBatchMixedAndFailures(t *testing.T) {
	d := workSemanticFixture(t)
	nodes := make([]any, 32)
	for i := range nodes {
		nodes[i] = map[string]any{"$work_standard": json.Number("1")}
	}
	nodes[7] = "preserved"
	if _, handled, err := d.workStandardArray(context.Background(), nodes, nil); handled || err != nil {
		t.Fatal("mixed array should fall back")
	}
	got, err := d.expandMetadata(context.Background(), nodes, make(referenceStack))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.([]any)[7], "preserved") {
		t.Fatal("mixed data lost")
	}
	nodes[7] = map[string]any{"$work_standard": json.Number("99")}
	if _, err = d.expandMetadata(context.Background(), nodes, make(referenceStack)); err == nil {
		t.Fatal("missing reference accepted")
	}
	nodes[7] = map[string]any{"$work_standard": json.Number("1")}
	if _, err = d.expandMetadata(context.Background(), nodes, referenceStack{referenceKey{"$work_standard", 1}: true}); err == nil {
		t.Fatal("active cycle ignored")
	}
	if _, err = d.db.Exec("UPDATE work_help_definitions SET translation_available=2"); err != nil {
		t.Fatal(err)
	}
	if _, err = d.expandMetadata(context.Background(), nodes, make(referenceStack)); err == nil {
		t.Fatal("corrupt boolean accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = d.runtimeValue(ctx, nodes); err == nil {
		t.Fatal("cancellation ignored")
	}
}
