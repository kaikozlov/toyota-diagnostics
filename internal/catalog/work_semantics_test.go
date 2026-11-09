package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func workSemanticFixture(t *testing.T) *DB {
	d := emptyDB(t)
	_, err := d.db.Exec(`CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 CREATE TABLE work_help_definitions(id INTEGER PRIMARY KEY,help_id INTEGER,kind INTEGER,namespace_id INTEGER,record INTEGER,source_id INTEGER,string_index INTEGER,string_source_id INTEGER,table_type INTEGER,text_id INTEGER,translation_available INTEGER);
 CREATE TABLE work_standard_definitions(id INTEGER PRIMARY KEY,exception_handler_flag INTEGER,exception_handler_id INTEGER,help_id INTEGER,record INTEGER,signal_id INTEGER,source_id INTEGER,standard_value_id_text INTEGER,standard_value_id INTEGER,standard_value_status_id INTEGER,table_type INTEGER,help_definition_id INTEGER);
 INSERT INTO text_values VALUES(1,'unresolved'),(2,'');
 INSERT INTO work_help_definitions VALUES(1,42,3,NULL,7,NULL,8,NULL,2,2,0);
 INSERT INTO work_standard_definitions VALUES(1,0,0,42,0,9007199254740993,NULL,NULL,1,1,18,1);
 ALTER TABLE work_standard_definitions ADD COLUMN unknown_bytes BLOB;
 ALTER TABLE work_help_definitions ADD COLUMN unknown_bytes BLOB;`)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestWorkStandardSemanticHelp(t *testing.T) {
	d := workSemanticFixture(t)
	result, err := d.workStandard(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	row := result.(map[string]any)
	if row["signal_id"] != json.Number("9007199254740993") || row["standard_value"] != nil || row["standard_value_status"] != "unresolved" {
		t.Fatal(row)
	}
	help := row["standard_value_help_records"].([]any)[0].(map[string]any)
	if help["text"] != "" || help["translation_available"] != false {
		t.Fatal(help)
	}
	if _, exists := row["uninterpreted_bytes"]; exists {
		t.Fatal("unexpected uninterpreted_bytes")
	}
	if _, err = d.workStandard(context.Background(), 99); err == nil {
		t.Fatal("missing definition accepted")
	}
	if _, err = d.db.Exec("DELETE FROM work_help_definitions"); err != nil {
		t.Fatal(err)
	}
	d.workStandards = cachedMetadata{}
	if _, err = d.workStandard(context.Background(), 1); err == nil {
		t.Fatal("missing help accepted")
	}
}
