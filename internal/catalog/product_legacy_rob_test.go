package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
)

const legacyROBFixture = `BEGIN TRANSACTION;
CREATE TABLE freeze_pattern_lists(id INTEGER PRIMARY KEY,entry_count INTEGER NOT NULL CHECK(entry_count>=0));
INSERT INTO "freeze_pattern_lists" VALUES(1,2);
CREATE TABLE freeze_pattern_members(list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id),position INTEGER NOT NULL,pattern_id INTEGER NOT NULL REFERENCES freeze_patterns(id),PRIMARY KEY(list_id,position)) WITHOUT ROWID;
INSERT INTO "freeze_pattern_members" VALUES(1,0,1);
INSERT INTO "freeze_pattern_members" VALUES(1,1,1);
CREATE TABLE freeze_patterns(id INTEGER PRIMARY KEY,record INTEGER NOT NULL,string_index INTEGER NOT NULL,table_type INTEGER NOT NULL,text_id INTEGER REFERENCES text_values(id),value INTEGER NOT NULL);
INSERT INTO "freeze_patterns" VALUES(1,2,6,14,5,0);
CREATE TABLE freeze_physical_defs(id INTEGER PRIMARY KEY,bit_width INTEGER NOT NULL,decimal_point_count INTEGER NOT NULL,div INTEGER NOT NULL,mul INTEGER NOT NULL,offset INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,record INTEGER NOT NULL,sign_mode INTEGER NOT NULL,table_type INTEGER NOT NULL,unit_key INTEGER NOT NULL,unit_id INTEGER NOT NULL REFERENCES freeze_units(id),pattern_list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id));
CREATE TABLE freeze_units(id INTEGER PRIMARY KEY,genre_id INTEGER NOT NULL,record INTEGER NOT NULL,string_index INTEGER NOT NULL,table_type INTEGER NOT NULL,text_id INTEGER REFERENCES text_values(id));
INSERT INTO "freeze_units" VALUES(1,0,61,70,15,NULL);
CREATE TABLE legacy_rob_definitions(id INTEGER PRIMARY KEY,
bdid INTEGER NOT NULL,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,graph_max INTEGER NOT NULL,graph_min INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,signal_key INTEGER NOT NULL,sort_key INTEGER NOT NULL,support_mode INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),short_name_id INTEGER REFERENCES text_values(id),decoder_id INTEGER REFERENCES text_values(id),support_rule_id INTEGER REFERENCES text_values(id)
,data_min REAL NOT NULL,data_max REAL NOT NULL,pattern_list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id),physical_count INTEGER NOT NULL);
INSERT INTO "legacy_rob_definitions" VALUES(1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,NULL,1,2,3,0.0,3.0,1,2);
CREATE TABLE legacy_rob_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_rob_definitions(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "legacy_rob_entries" VALUES(1,1,4,45,1);
CREATE TABLE legacy_rob_physical(id INTEGER PRIMARY KEY,
decimal_point_count INTEGER NOT NULL,div INTEGER NOT NULL,mul INTEGER NOT NULL,offset INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,record INTEGER NOT NULL,sign_mode INTEGER NOT NULL,table_number INTEGER NOT NULL,unit_key INTEGER NOT NULL,unit_id INTEGER NOT NULL REFERENCES freeze_units(id));
INSERT INTO "legacy_rob_physical" VALUES(1,0,1,1,0,12,4,0,13,5,1);
CREATE TABLE legacy_rob_variants(definition_id INTEGER NOT NULL REFERENCES legacy_rob_definitions(id),position INTEGER NOT NULL,physical_id INTEGER NOT NULL REFERENCES legacy_rob_physical(id),PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
INSERT INTO "legacy_rob_variants" VALUES(1,0,1);
INSERT INTO "legacy_rob_variants" VALUES(1,1,1);
CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT UNIQUE);
INSERT INTO "text_values" VALUES(1,'');
INSERT INTO "text_values" VALUES(2,'legacy-rob-msb0-u32-v1');
INSERT INTO "text_values" VALUES(3,'excluded');
INSERT INTO "text_values" VALUES(4,'unit');
INSERT INTO "text_values" VALUES(5,'ready');
COMMIT;`

func TestLegacyROBSignalRoundtrip(t *testing.T) {
	s, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.SetMaxOpenConns(1)
	if _, e = s.Exec(legacyROBFixture); e != nil {
		t.Fatal(e)
	}
	d := &DB{db: s}
	got, e := d.legacyROBSignal(context.Background(), 1)
	if e != nil {
		t.Fatal(e)
	}
	encoded, e := json.Marshal(got)
	if e != nil {
		t.Fatal(e)
	}
	var actual, expected any
	json.Unmarshal(encoded, &actual)
	json.Unmarshal([]byte(`{"bdid": 0, "bit_end": 0, "bit_start": 0, "exception_handler_flag": 0, "exception_handler_id": 0, "graph_max": 0, "graph_min": 0, "name_string_index": 0, "pattern_display_key": 0, "physical_data_key": 0, "presentation_mode": 0, "signal_key": 0, "sort_key": 0, "support_mode": 0, "name": null, "short_name": "", "decoder": "legacy-rob-msb0-u32-v1", "support_rule": "excluded", "source": "unit", "table": 45, "record": 1, "data_min": 0.0, "data_max": 3.0, "physical_variants": [{"decimal_point_count": 0, "div": 1, "mul": 1, "offset": 0, "physical_data_key": 12, "record": 4, "sign_mode": 0, "table": 13, "unit_key": 5, "units": [{"genre_id": 0, "name": null, "name_string_index": 70, "record": 61}]}, {"decimal_point_count": 0, "div": 1, "mul": 1, "offset": 0, "physical_data_key": 12, "record": 4, "sign_mode": 0, "table": 13, "unit_key": 5, "units": [{"genre_id": 0, "name": null, "name_string_index": 70, "record": 61}]}], "pattern_display": [{"name": "ready", "name_string_index": 6, "record": 2, "table": 14, "value": 0}, {"name": "ready", "name_string_index": 6, "record": 2, "table": 14, "value": 0}]}`), &expected)
	runtime, err := d.runtimeValue(context.Background(), map[string]any{"$legacy_rob_signal": json.Number("1")})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := runtime.(map[string]any)["bit_start"].(float64); !ok {
		t.Fatalf("unexpected operational number type: %T", runtime.(map[string]any)["bit_start"])
	}
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("roundtrip mismatch: %s", encoded)
	}
	if _, e = s.Exec("DELETE FROM legacy_rob_variants WHERE position=1"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.legacyROBSignal(context.Background(), 1); e == nil {
		t.Fatal("missing ordered relation accepted")
	}
}
