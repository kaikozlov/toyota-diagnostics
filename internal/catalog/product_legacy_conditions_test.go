package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
)

func legacyConditionFixture(t *testing.T) *DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	_, err = conn.Exec(`CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 CREATE TABLE legacy_condition_defs(id INTEGER PRIMARY KEY,condition_key INTEGER,variable_id INTEGER,bit_start INTEGER,bit_end INTEGER,source_mode INTEGER,exception_id INTEGER,exception_flag INTEGER,variable BLOB);
 CREATE TABLE legacy_condition_entries(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER,has_kind INTEGER);
 CREATE TABLE legacy_response_defs(id INTEGER PRIMARY KEY,response_key INTEGER,data_length INTEGER,exception_id INTEGER,exception_flag INTEGER);
 CREATE TABLE legacy_response_entries(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER);
 CREATE TABLE legacy_dynamic_defs(id INTEGER PRIMARY KEY,monitor_key INTEGER,mul INTEGER,div INTEGER,bit_start INTEGER,bit_end INTEGER,pid INTEGER);
 CREATE TABLE legacy_dynamic_entries(id INTEGER PRIMARY KEY,definition_id INTEGER,record_number INTEGER,unknown_id INTEGER);
 CREATE TABLE legacy_dynamic_unknown_facts(id INTEGER PRIMARY KEY,range_14_16_id INTEGER,range_18_end_id INTEGER);
 INSERT INTO text_values VALUES(1,'ECU.ddb');
 INSERT INTO legacy_condition_defs VALUES(1,1,10001,0,7,6,0,0,x''),(2,2,10002,0,7,4,0,0,x'ffbe');
 INSERT INTO legacy_condition_entries VALUES(1,1,1,9,0,1),(2,2,1,8,1,0);
 INSERT INTO legacy_response_defs VALUES(1,2,0,0,0);
 INSERT INTO legacy_response_entries VALUES(1,1,1,6,9);
 INSERT INTO legacy_dynamic_defs VALUES(1,314,-10,255,24,31,79);
 INSERT INTO legacy_dynamic_entries VALUES(1,1,5,NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	return &DB{db: conn}
}
func TestLegacyConditionEmptyVariableAndPatches(t *testing.T) {
	d := legacyConditionFixture(t)
	ctx := context.Background()
	value, err := d.legacyCondition(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	r := value.(map[string]any)
	if r["variable"] != "" || r["native_status"] != "invalid_variable_length" {
		t.Fatal(r)
	}
	value, err = d.legacyCondition(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	r = value.(map[string]any)
	want := []any{map[string]any{"offset": json.Number("2"), "value": json.Number("255")}, map[string]any{"offset": json.Number("3"), "value": json.Number("190")}}
	if !reflect.DeepEqual(r["request_patches"], want) {
		t.Fatal(r)
	}
	if _, ok := r["kind"]; ok {
		t.Fatal("invented root kind")
	}
}
func TestLegacySemanticRuntimeAndMissing(t *testing.T) {
	d := legacyConditionFixture(t)
	ctx := context.Background()
	value, err := d.runtimeValue(ctx, map[string]any{"$legacy_dynamic": json.Number("1")})
	if err != nil {
		t.Fatal(err)
	}
	r := value.(map[string]any)
	if r["mul"] != float64(-10) {
		t.Fatalf("wrong runtime type %#v", r)
	}
	r["mul"] = float64(0)
	value, err = d.legacyDynamic(ctx, 1)
	if err != nil || value.(map[string]any)["mul"] != json.Number("-10") {
		t.Fatal("cache mutation", value, err)
	}
	value, err = d.runtimeValue(ctx, map[string]any{"$legacy_response_definition": json.Number("1")})
	if err != nil || value.(map[string]any)["length_resolution"] != "runtime" {
		t.Fatal(value, err)
	}
	if _, err = d.legacyCondition(ctx, 999); err == nil {
		t.Fatal("accepted missing condition")
	}
}
func TestLegacyInvalidModeRemainsUnsupported(t *testing.T) {
	row, err := legacyConditionValue([7]int64{1, 1, 0, 7, 99, 0, 0}, nil, false, "ECU.ddb", 8, 0, 0)
	if err != nil || row["query"] != nil || row["native_status"] != "invalid_geometry_or_mode" {
		t.Fatal(row, err)
	}
	row, err = legacyConditionValue([7]int64{1, 1, 7, 0, 2, 0, 0}, []byte{1}, true, "ECU.ddb", 8, 0, 0)
	if err != nil || row["native_status"] != "invalid_geometry_or_mode" {
		t.Fatal(row, err)
	}
}
