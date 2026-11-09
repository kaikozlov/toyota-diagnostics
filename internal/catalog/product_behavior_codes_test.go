package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func behaviorFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	_, e := d.db.Exec(`CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 INSERT INTO text_values VALUES(1,'Engine.ddb'),(2,'Condition'),(3,'X0500'),(4,'behavior'),(5,'');
 CREATE TABLE behavior_code_defs(id INTEGER PRIMARY KEY,variant INTEGER,behavior_code INTEGER,comment_string_index INTEGER,division_string_index INTEGER,enabled INTEGER,exception_handler_flag INTEGER,exception_handler_id INTEGER,memory_override INTEGER,name_string_index INTEGER,name_id INTEGER,comment_id INTEGER,signature_id INTEGER,division_id INTEGER,kind_id INTEGER);
 CREATE TABLE behavior_code_entries(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER);
 INSERT INTO behavior_code_defs VALUES(1,0,1280,NULL,NULL,NULL,NULL,NULL,NULL,NULL,2,NULL,3,NULL,4);
 INSERT INTO behavior_code_defs VALUES(2,1,1280,0,0,0,1,5,0,9,2,5,3,NULL,NULL);
 INSERT INTO behavior_code_entries VALUES(1,1,1,87,0),(2,2,1,163,1);
 ALTER TABLE behavior_code_defs ADD COLUMN unknown_bytes BLOB;`)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestBehaviorCodeVariantsRuntime(t *testing.T) {
	d := behaviorFixture(t)
	ctx := context.Background()
	old, e := d.behaviorCode(ctx, 1)
	if e != nil {
		t.Fatal(e)
	}
	if old.(map[string]any)["comment"] != nil || old.(map[string]any)["kind"] != "behavior" {
		t.Fatal(old)
	}
	value, e := d.runtimeValue(ctx, map[string]any{"$behavior_code": json.Number("2")})
	if e != nil {
		t.Fatal(e)
	}
	m := value.(map[string]any)
	if m["enabled"] != float64(0) || m["comment"] != "" || m["division"] != nil {
		t.Fatal(m)
	}
	if _, ok := m["kind"]; ok {
		t.Fatal("P5 field leaked")
	}
	m["name"] = "changed"
	again, e := d.runtimeValue(ctx, map[string]any{"$behavior_code": json.Number("2")})
	if e != nil || again.(map[string]any)["name"] != "Condition" {
		t.Fatal("cache mutation", e)
	}
}
func TestBehaviorCodeRejectsMissingFacts(t *testing.T) {
	for _, q := range []string{"DELETE FROM text_values WHERE id=2", "DELETE FROM behavior_code_defs WHERE id=1", "UPDATE behavior_code_defs SET enabled=1 WHERE id=1", "UPDATE behavior_code_defs SET variant=9 WHERE id=1"} {
		d := behaviorFixture(t)
		if _, e := d.db.Exec(q); e != nil {
			t.Fatal(e)
		}
		if _, e := d.behaviorCode(context.Background(), 1); e == nil {
			t.Fatal("corruption accepted", q)
		}
	}
}
