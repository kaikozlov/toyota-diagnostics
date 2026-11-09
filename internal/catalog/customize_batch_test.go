package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestCustomizeBatchGroups(t *testing.T) {
	d := emptyDB(t)
	cols := []string{"id INTEGER PRIMARY KEY"}
	for _, f := range customizeFields(customizeGroupNames, false) {
		cols = append(cols, f.column+" INTEGER")
	}
	if _, e := d.db.Exec("CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);CREATE TABLE customize_group_definitions(" + strings.Join(cols, ",") + ");INSERT INTO customize_group_definitions VALUES(1,0,0,0,9007199254740993,NULL,0,0),(2,0,0,0,2,NULL,0,0)"); e != nil {
		t.Fatal(e)
	}
	nodes := make([]any, 300)
	for i := range nodes {
		nodes[i] = map[string]any{"$customize_group": json.Number("1")}
		if i%2 == 1 {
			nodes[i] = map[string]any{"$customize_group": json.Number("2")}
		}
	}
	value, e := d.expandMetadata(context.Background(), nodes, referenceStack{})
	if e != nil {
		t.Fatal(e)
	}
	out := value.([]any)
	if out[0].(map[string]any)["group_id"] != json.Number("9007199254740993") {
		t.Fatal(out[0])
	}
	out[0].(map[string]any)["group_id"] = "changed"
	if out[2].(map[string]any)["group_id"] == "changed" {
		t.Fatal("mutable duplicate aliases")
	}
	active := referenceStack{referenceKey{"$customize_group", 1}: true}
	if _, _, e = d.customizeArray(context.Background(), nodes, active); e == nil {
		t.Fatal("cycle accepted")
	}
	nodes[0] = map[string]any{"other": 1}
	if _, handled, e := d.customizeArray(context.Background(), nodes, nil); handled || e != nil {
		t.Fatal("mixed array intercepted")
	}
	nodes[0] = map[string]any{"$customize_group": json.Number("999")}
	if _, _, e = d.customizeArray(context.Background(), nodes, nil); e == nil {
		t.Fatal("missing accepted")
	}
	nodes[0] = map[string]any{"$customize_group": json.Number("1")}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, e = d.customizeArray(ctx, nodes, nil); e == nil {
		t.Fatal("cancellation ignored")
	}
}
