package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func planFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	_, err := d.db.Exec(`CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT NOT NULL);
 INSERT INTO text_values VALUES(1,'unsupported'),(2,'routine'),(3,'Example'),(4,'Refused safely');
 CREATE TABLE plan_refused(id INTEGER PRIMARY KEY,present INTEGER,f_execution INTEGER,f_kind INTEGER,f_name INTEGER,f_id INTEGER,f_record INTEGER,f_routine_id INTEGER,f_error INTEGER);
 INSERT INTO plan_refused VALUES(1,0,1,2,3,9007199254740993,5,NULL,4);
 CREATE TABLE operation_plan_entries(id INTEGER PRIMARY KEY,direct_id INTEGER,routine_id INTEGER,refused_id INTEGER);
 INSERT INTO operation_plan_entries VALUES(1,NULL,NULL,1);
 CREATE TABLE plan_initial_read(id INTEGER PRIMARY KEY,present INTEGER,f_mode INTEGER,f_frames INTEGER,f_name INTEGER,f_resolved INTEGER,f_selector INTEGER);
 CREATE TABLE plan_initial_read_frames(parent_id INTEGER,position INTEGER,value INTEGER);
 CREATE TABLE operation_frames(id INTEGER PRIMARY KEY);
 INSERT INTO operation_frames VALUES(1);
 INSERT INTO plan_initial_read VALUES(1,15,1,1,3,1,2);
 INSERT INTO plan_initial_read_frames VALUES(1,0,1);
 CREATE TABLE plan_variable(id INTEGER PRIMARY KEY,present INTEGER,f_bytes BLOB,f_id INTEGER,f_normalized_id INTEGER);
 INSERT INTO plan_variable VALUES(1,0,X'',3,2);
 `)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestOperationPlanRefusalAndRuntime(t *testing.T) {
	d := planFixture(t)
	ctx := context.Background()
	got, err := d.operationPlan(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	row := got.(map[string]any)
	if row["id"] != json.Number("9007199254740993") || row["error"] != "Refused safely" {
		t.Fatal(row)
	}
	if _, ok := row["routine_id"]; ok {
		t.Fatal("absent optional field returned")
	}
	runtime, err := d.runtimeValue(ctx, map[string]any{"$operation_plan": json.Number("1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.(map[string]any)["record"].(float64); !ok {
		t.Fatalf("not ordinary runtime number: %#v", runtime)
	}
	runtime.(map[string]any)["error"] = "mutated"
	again, err := d.runtimeValue(ctx, map[string]any{"$operation_plan": json.Number("1")})
	if err != nil || again.(map[string]any)["error"] != "Refused safely" {
		t.Fatal("cache leaked mutation", err)
	}
}
func TestOperationPlanCountPresenceAndReferences(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"missing child", "DELETE FROM plan_initial_read_frames"},
		{"position gap", "UPDATE plan_initial_read_frames SET position=1"},
		{"missing reference", "DELETE FROM operation_frames"},
		{"bad mask", "UPDATE plan_initial_read SET present=1"},
		{"bad boolean", "UPDATE plan_initial_read SET f_resolved=2"},
		{"absent value", "UPDATE plan_initial_read SET present=0"},
		{"bad count", "UPDATE plan_initial_read SET f_frames=2"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := planFixture(t)
			if _, err := d.db.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			if _, err := d.planObject(context.Background(), "initial_read", 1, 0); err == nil {
				t.Fatal("corruption accepted")
			}
		})
	}
	d := planFixture(t)
	got, err := d.planObject(context.Background(), "initial_read", 1, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := []any{map[string]any{"$operation_frame": json.Number("1")}}
	if !reflect.DeepEqual(got["frames"], want) {
		t.Fatalf("%#v", got)
	}
}
func TestOperationPlanBytesAndMissingRoot(t *testing.T) {
	d := planFixture(t)
	ctx := context.Background()
	v, err := d.planObject(ctx, "variable", 1, 0)
	if err != nil || v["bytes"] != "" {
		t.Fatal(v, err)
	}
	if _, err := d.db.Exec("UPDATE plan_variable SET f_bytes='not bytes'"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.planObject(ctx, "variable", 1, 0); err == nil {
		t.Fatal("nonbinary command accepted")
	}
	if _, err := d.operationPlan(ctx, 99); err == nil {
		t.Fatal("missing root accepted")
	}
	if _, err := d.db.Exec("UPDATE operation_plan_entries SET direct_id=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.operationPlan(ctx, 1); err == nil {
		t.Fatal("ambiguous root accepted")
	}
}
