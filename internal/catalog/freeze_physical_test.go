package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFreezePhysicalSemanticDefinition(t *testing.T) {
	d := emptyDB(t)
	_, err := d.db.Exec(`CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 CREATE TABLE freeze_units(id INTEGER PRIMARY KEY,genre_id INTEGER,record INTEGER,string_index INTEGER,table_type INTEGER,text_id INTEGER);
 CREATE TABLE freeze_patterns(id INTEGER PRIMARY KEY,record INTEGER,string_index INTEGER,table_type INTEGER,text_id INTEGER,value INTEGER);
 CREATE TABLE freeze_pattern_lists(id INTEGER PRIMARY KEY,entry_count INTEGER);
 CREATE TABLE freeze_pattern_members(list_id INTEGER,position INTEGER,pattern_id INTEGER);
 CREATE TABLE freeze_physical_defs(id INTEGER PRIMARY KEY,bit_width INTEGER,decimal_point_count INTEGER,div INTEGER,mul INTEGER,offset INTEGER,physical_data_key INTEGER,record INTEGER,sign_mode INTEGER,table_type INTEGER,unit_key INTEGER,unit_id INTEGER,pattern_list_id INTEGER);
 INSERT INTO text_values VALUES(1,'');
 INSERT INTO freeze_units VALUES(1,0,7,8,15,NULL);
 INSERT INTO freeze_patterns VALUES(1,9,10,14,1,-1);
 INSERT INTO freeze_pattern_lists VALUES(1,2);
 INSERT INTO freeze_pattern_members VALUES(1,0,1),(1,1,1);
 INSERT INTO freeze_physical_defs VALUES(1,8,2,0,9007199254740993,-3,4,5,2,13,6,1,1);`)
	if err != nil {
		t.Fatal(err)
	}
	result, err := d.freezePhysical(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	row := result.(map[string]any)
	if row["mul"] != json.Number("9007199254740993") || row["div"] != json.Number("0") {
		t.Fatal(row)
	}
	if row["units"].([]any)[0].(map[string]any)["text"] != nil {
		t.Fatal(row)
	}
	patterns := row["patterns"].([]any)
	if len(patterns) != 2 || patterns[0].(map[string]any)["text"] != "" {
		t.Fatal(patterns)
	}
	_, err = d.db.Exec("DELETE FROM freeze_pattern_members WHERE position=1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = d.freezePhysical(context.Background(), 1); err == nil {
		t.Fatal("missing tail accepted")
	}
	if _, err = d.freezePhysical(context.Background(), 99); err == nil {
		t.Fatal("missing definition accepted")
	}
}
