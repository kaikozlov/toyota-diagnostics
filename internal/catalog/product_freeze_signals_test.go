package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func productFreezeFixture(t *testing.T) *DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	cols := []string{}
	for _, k := range append(append([]string{}, freezeSignalIntegers...), freezeSignalBooleans...) {
		cols = append(cols, k+" INTEGER NOT NULL DEFAULT 0")
	}
	_, err = conn.Exec(fmt.Sprintf(`CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 INSERT INTO text_values VALUES(1,'name'),(2,''),(3,'gtsplus-104'),(4,'ECU.ddb');
 CREATE TABLE legacy_freeze_definitions(id INTEGER PRIMARY KEY,name_id INTEGER,short_name_id INTEGER,layout_id INTEGER,physical_id INTEGER,%s,data_min REAL,data_max REAL,support_count INTEGER,dynamic_count INTEGER);
 CREATE TABLE legacy_freeze_entries(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER,unknown_id INTEGER);
 CREATE TABLE legacy_freeze_unknown_facts(id INTEGER PRIMARY KEY,bytes_34_3f BLOB,bytes_48_4b BLOB,bytes_56_57 BLOB,bytes_5e_5f BLOB,byte_67 INTEGER);
 CREATE TABLE legacy_freeze_support_members(definition_id INTEGER,position INTEGER,condition_id INTEGER);
 CREATE TABLE legacy_freeze_dynamic_members(definition_id INTEGER,position INTEGER,dynamic_id INTEGER);
 INSERT INTO legacy_freeze_definitions(id,name_id,short_name_id,layout_id,physical_id,data_min,data_max,support_count,dynamic_count) VALUES(1,NULL,2,3,1,0.0,127.0,2,0);
 INSERT INTO legacy_freeze_entries VALUES(1,1,4,10,2,NULL);
 INSERT INTO legacy_freeze_support_members VALUES(1,0,4),(1,1,4);`, strings.Join(cols, ",")))
	if err != nil {
		t.Fatal(err)
	}
	return &DB{db: conn}
}
func TestProductFreezeSemanticFields(t *testing.T) {
	d := productFreezeFixture(t)
	value, err := d.legacyFreezeSignal(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	row := value.(map[string]any)
	if row["name"] != nil || row["record"] != json.Number("2") || row["data_min"] != json.Number("0.0") {
		t.Fatal(row)
	}
	if len(row["support_conditions"].([]any)) != 2 {
		t.Fatal("lost duplicate child")
	}
	if row["kind"] != "freeze_frame" || row["freeze_member"] != false {
		t.Fatal(row)
	}
}
func TestProductFreezeMissingRelationship(t *testing.T) {
	d := productFreezeFixture(t)
	if _, err := d.db.Exec("DELETE FROM legacy_freeze_support_members WHERE position=1"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.legacyFreezeSignal(context.Background(), 1); err == nil {
		t.Fatal("accepted lost condition")
	}
}
