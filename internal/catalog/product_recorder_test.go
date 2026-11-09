package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

const recorderFixture = `BEGIN TRANSACTION;
CREATE TABLE ddr_behavior_code_defs(id INTEGER PRIMARY KEY,additional_string_index INTEGER NOT NULL,behavior_key INTEGER NOT NULL,dependent_data_0 INTEGER NOT NULL,dependent_data_1 INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,trigger_memo_string_index INTEGER NOT NULL,trigger_selector_0 INTEGER NOT NULL,trigger_selector_1 INTEGER NOT NULL,additional_text_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),trigger_memo_id INTEGER REFERENCES text_values(id));
INSERT INTO "ddr_behavior_code_defs" VALUES(1,0,0,0,0,0,0,0,0,0,0,NULL,NULL,NULL,NULL);
CREATE TABLE ddr_behavior_code_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_behavior_code_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "ddr_behavior_code_entries" VALUES(1,1,1,167,3);
CREATE TABLE ddr_behavior_condition_defs(id INTEGER PRIMARY KEY,combination_mode INTEGER NOT NULL,comparison_mode INTEGER NOT NULL,condition_order INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,referenced_signal_key INTEGER NOT NULL,signal_key INTEGER NOT NULL,comparison_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),comparison_bits64 TEXT NOT NULL);
INSERT INTO "ddr_behavior_condition_defs" VALUES(1,0,0,0,0,0,0,0,NULL,NULL,'18446744073709551615');
CREATE TABLE ddr_behavior_condition_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_behavior_condition_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "ddr_behavior_condition_entries" VALUES(1,1,1,167,3);
CREATE TABLE ddr_behavior_signal_defs(id INTEGER PRIMARY KEY,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,did INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,signal_key INTEGER NOT NULL,signal_role INTEGER NOT NULL,sort_key INTEGER NOT NULL,extraction_mode INTEGER NOT NULL,graph_max INTEGER NOT NULL,graph_min INTEGER NOT NULL,support_condition_key INTEGER NOT NULL,live_list_member INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),data_min REAL NOT NULL,data_max REAL NOT NULL,signal_info_id INTEGER NOT NULL REFERENCES signal_infos(id),invalid_condition_records_count INTEGER NOT NULL);
INSERT INTO "ddr_behavior_signal_defs" VALUES(1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,NULL,NULL,0.0,0.0,1,3);
CREATE TABLE ddr_behavior_signal_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_behavior_signal_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "ddr_behavior_signal_entries" VALUES(1,1,1,167,3);
CREATE TABLE ddr_behavior_signal_invalid_condition_records(definition_id INTEGER NOT NULL REFERENCES ddr_behavior_signal_defs(id),position INTEGER NOT NULL,value INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
INSERT INTO "ddr_behavior_signal_invalid_condition_records" VALUES(1,0,2);
INSERT INTO "ddr_behavior_signal_invalid_condition_records" VALUES(1,1,2);
INSERT INTO "ddr_behavior_signal_invalid_condition_records" VALUES(1,2,0);
CREATE TABLE ddr_condition_defs(id INTEGER PRIMARY KEY,combination_mode INTEGER NOT NULL,comparison_mode INTEGER NOT NULL,comparison_value INTEGER NOT NULL,condition_order INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,referenced_signal_key INTEGER NOT NULL,signal_key INTEGER NOT NULL,comparison_id INTEGER REFERENCES text_values(id),unknown_byte15 TEXT);
INSERT INTO "ddr_condition_defs" VALUES(1,0,0,0,0,0,0,0,0,NULL,NULL);
CREATE TABLE ddr_condition_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_condition_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "ddr_condition_entries" VALUES(1,1,1,167,3);
CREATE TABLE ddr_data_id_defs(id INTEGER PRIMARY KEY,did INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,payload_length INTEGER NOT NULL,validity_mode INTEGER NOT NULL);
INSERT INTO "ddr_data_id_defs" VALUES(1,0,0,0,0,0);
CREATE TABLE ddr_data_id_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_data_id_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "ddr_data_id_entries" VALUES(1,1,1,167,3);
CREATE TABLE ddr_signal_data_id_records(definition_id INTEGER NOT NULL REFERENCES ddr_signal_defs(id),position INTEGER NOT NULL,value INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
INSERT INTO "ddr_signal_data_id_records" VALUES(1,0,2);
INSERT INTO "ddr_signal_data_id_records" VALUES(1,1,2);
INSERT INTO "ddr_signal_data_id_records" VALUES(1,2,0);
CREATE TABLE ddr_signal_defs(id INTEGER PRIMARY KEY,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,did INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,signal_key INTEGER NOT NULL,signal_role INTEGER NOT NULL,sort_key INTEGER NOT NULL,visibility INTEGER NOT NULL,list_member INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),data_min REAL NOT NULL,data_max REAL NOT NULL,signal_info_id INTEGER NOT NULL REFERENCES signal_infos(id),data_id_records_count INTEGER NOT NULL,invalid_condition_records_count INTEGER NOT NULL);
INSERT INTO "ddr_signal_defs" VALUES(1,0,0,0,0,0,0,0,0,0,0,0,0,0,0,NULL,0.0,0.0,1,3,3);
CREATE TABLE ddr_signal_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_signal_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "ddr_signal_entries" VALUES(1,1,1,167,3);
CREATE TABLE ddr_signal_invalid_condition_records(definition_id INTEGER NOT NULL REFERENCES ddr_signal_defs(id),position INTEGER NOT NULL,value INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
INSERT INTO "ddr_signal_invalid_condition_records" VALUES(1,0,2);
INSERT INTO "ddr_signal_invalid_condition_records" VALUES(1,1,2);
INSERT INTO "ddr_signal_invalid_condition_records" VALUES(1,2,0);
CREATE TABLE ddr_trigger_defs(id INTEGER PRIMARY KEY,dtc_identifier INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,trigger_memo_string_index INTEGER NOT NULL,trigger_point_enabled INTEGER NOT NULL,trigger_selector_0 INTEGER NOT NULL,trigger_selector_1 INTEGER NOT NULL,unresolved_u16_0e INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),trigger_memo_id INTEGER REFERENCES text_values(id));
INSERT INTO "ddr_trigger_defs" VALUES(1,0,0,0,0,0,0,0,0,0,NULL,NULL);
CREATE TABLE ddr_trigger_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_trigger_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "ddr_trigger_entries" VALUES(1,1,1,167,3);
CREATE TABLE signal_infos(id INTEGER PRIMARY KEY);
INSERT INTO "signal_infos" VALUES(1);
CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT UNIQUE);
INSERT INTO "text_values" VALUES(1,'ECU');
COMMIT;`

func TestProductRecorderConditionUnknownByte(t *testing.T) {
	s, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.SetMaxOpenConns(1)
	if _, err = s.Exec(recorderFixture); err != nil {
		t.Fatal(err)
	}
	d := &DB{db: s}
	ctx := context.Background()
	for _, value := range []any{nil, "00", "a5", "ff"} {
		if _, err = s.Exec("UPDATE ddr_condition_defs SET unknown_byte15=?", value); err != nil {
			t.Fatal(err)
		}
		got, err := d.recorderRecord(ctx, "ddr_condition", 1)
		if err != nil {
			t.Fatal(err)
		}
		row := got.(map[string]any)
		if value == nil {
			if _, ok := row["uninterpreted_bytes"]; ok {
				t.Fatal("absent byte became zero")
			}
		} else if !reflect.DeepEqual(row["uninterpreted_bytes"], map[string]any{"15": value}) {
			t.Fatalf("unknown byte changed: %#v", row)
		}
	}
	for _, value := range []string{"", "0", "GG", "FF", " 0", "0000"} {
		if _, err = s.Exec("UPDATE ddr_condition_defs SET unknown_byte15=?", value); err != nil {
			t.Fatal(err)
		}
		if _, err = d.recorderRecord(ctx, "ddr_condition", 1); err == nil {
			t.Fatalf("accepted malformed byte %q", value)
		}
	}
}

func TestProductRecorderRoundtrip(t *testing.T) {
	s, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.SetMaxOpenConns(1)
	if _, e = s.Exec(recorderFixture); e != nil {
		t.Fatal(e)
	}
	d := &DB{db: s}
	var expected map[string]any
	json.Unmarshal([]byte(`{"ddr_signal": {"bit_end": 0, "bit_start": 0, "did": 0, "exception_flag": 0, "exception_id": 0, "name_string_index": 0, "pattern_display_key": 0, "physical_data_key": 0, "presentation_mode": 0, "signal_key": 0, "signal_role": 0, "sort_key": 0, "visibility": 0, "name": null, "list_member": false, "data_min": 0.0, "data_max": 0.0, "signal_info": {"$signal_info": 1}, "data_id_records": [2, 2, 0], "invalid_condition_records": [2, 2, 0], "source": "ECU", "table": 167, "record": 3}, "ddr_behavior_signal": {"bit_end": 0, "bit_start": 0, "did": 0, "exception_flag": 0, "exception_id": 0, "name_string_index": 0, "pattern_display_key": 0, "physical_data_key": 0, "presentation_mode": 0, "signal_key": 0, "signal_role": 0, "sort_key": 0, "extraction_mode": 0, "graph_max": 0, "graph_min": 0, "support_condition_key": 0, "name": null, "family": null, "live_list_member": false, "data_min": 0.0, "data_max": 0.0, "signal_info": {"$signal_info": 1}, "invalid_condition_records": [2, 2, 0], "source": "ECU", "table": 167, "record": 3}, "ddr_condition": {"combination_mode": 0, "comparison_mode": 0, "comparison_value": 0, "condition_order": 0, "exception_flag": 0, "exception_id": 0, "referenced_signal_key": 0, "signal_key": 0, "comparison": null, "source": "ECU", "table": 167, "record": 3}, "ddr_behavior_condition": {"combination_mode": 0, "comparison_mode": 0, "condition_order": 0, "exception_flag": 0, "exception_id": 0, "referenced_signal_key": 0, "signal_key": 0, "comparison": null, "family": null, "comparison_bits64": 18446744073709551615, "source": "ECU", "table": 167, "record": 3}, "ddr_data_id": {"did": 0, "exception_flag": 0, "exception_id": 0, "payload_length": 0, "validity_mode": 0, "source": "ECU", "table": 167, "record": 3}, "ddr_trigger": {"dtc_identifier": 0, "exception_flag": 0, "exception_id": 0, "name_string_index": 0, "trigger_memo_string_index": 0, "trigger_point_enabled": 0, "trigger_selector_0": 0, "trigger_selector_1": 0, "unresolved_u16_0e": 0, "name": null, "trigger_memo": null, "source": "ECU", "table": 167, "record": 3}, "ddr_behavior_code": {"additional_string_index": 0, "behavior_key": 0, "dependent_data_0": 0, "dependent_data_1": 0, "exception_flag": 0, "exception_id": 0, "name_string_index": 0, "trigger_memo_string_index": 0, "trigger_selector_0": 0, "trigger_selector_1": 0, "additional_text": null, "family": null, "name": null, "trigger_memo": null, "source": "ECU", "table": 167, "record": 3}}`), &expected)
	for kind, want := range expected {
		got, e := d.recorderRecord(context.Background(), kind, 1)
		if e != nil {
			t.Fatal(kind, e)
		}
		encoded, e := json.Marshal(got)
		if e != nil {
			t.Fatal(e)
		}
		var actual any
		json.Unmarshal(encoded, &actual)
		if !reflect.DeepEqual(actual, want) {
			t.Fatalf("%s roundtrip mismatch: %s", kind, encoded)
		}
		if kind == "ddr_behavior_condition" && !strings.Contains(string(encoded), "18446744073709551615") {
			t.Fatal("uint64 comparison lost precision")
		}
	}
	runtime, e := d.runtimeValue(context.Background(), map[string]any{"$ddr_condition": json.Number("1")})
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := runtime.(map[string]any)["signal_key"].(float64); !ok {
		t.Fatal("wrong operational type")
	}
	if _, e = s.Exec("DELETE FROM ddr_signal_data_id_records WHERE position=1"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.recorderRecord(context.Background(), "ddr_signal", 1); e == nil {
		t.Fatal("missing ordered relation accepted")
	}
}
