package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
)

const nativeRecorderFixture = `BEGIN TRANSACTION;
CREATE TABLE native_recorder_addresses_defs(id INTEGER PRIMARY KEY,address INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,monitor_key INTEGER NOT NULL,record_length INTEGER NOT NULL,section_kind INTEGER NOT NULL,trigger_id INTEGER NOT NULL,header_member INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_13_15_id INTEGER REFERENCES text_values(id),relation_count INTEGER NOT NULL CHECK(relation_count>=0));
INSERT INTO "native_recorder_addresses_defs" VALUES(1,0,0,0,1,0,1,2,1,1,2,1,3,1);
CREATE TABLE native_recorder_addresses_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_addresses_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "native_recorder_addresses_entries" VALUES(1,1,4,52,0);
CREATE TABLE native_recorder_addresses_links(definition_id INTEGER NOT NULL REFERENCES native_recorder_addresses_defs(id),position INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
INSERT INTO "native_recorder_addresses_links" VALUES(1,0,0,50);
CREATE TABLE native_recorder_case_data_ids_defs(id INTEGER PRIMARY KEY,case_did INTEGER NOT NULL,case_parameter INTEGER NOT NULL,case_selector INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,header_did INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_9_id INTEGER REFERENCES text_values(id),unknown_11_id INTEGER REFERENCES text_values(id));
INSERT INTO "native_recorder_case_data_ids_defs" VALUES(1,5377,36,1,0,0,5136,14,15,1,7,7);
CREATE TABLE native_recorder_case_data_ids_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_case_data_ids_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "native_recorder_case_data_ids_entries" VALUES(1,1,16,109,0);
CREATE TABLE native_recorder_header_data_ids_defs(id INTEGER PRIMARY KEY,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,header_did INTEGER NOT NULL,header_parameter INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_7_id INTEGER REFERENCES text_values(id));
INSERT INTO "native_recorder_header_data_ids_defs" VALUES(1,0,0,5136,62,14,17,1,7);
CREATE TABLE native_recorder_header_data_ids_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_header_data_ids_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "native_recorder_header_data_ids_entries" VALUES(1,1,16,108,0);
CREATE TABLE native_recorder_invalid_conditions_defs(id INTEGER PRIMARY KEY,combination_mode INTEGER NOT NULL,comparison_mode INTEGER NOT NULL,comparison_value INTEGER NOT NULL,condition_order INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,referenced_signal_key INTEGER NOT NULL,signal_key INTEGER NOT NULL,comparison_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_15_id INTEGER REFERENCES text_values(id));
INSERT INTO "native_recorder_invalid_conditions_defs" VALUES(1,0,1,254,1,0,0,3,3,5,1,6,1,7);
CREATE TABLE native_recorder_invalid_conditions_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_invalid_conditions_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "native_recorder_invalid_conditions_entries" VALUES(1,1,4,53,0);
CREATE TABLE native_recorder_monitors_defs(id INTEGER PRIMARY KEY,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,data_max INTEGER NOT NULL,data_min INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,graph_max INTEGER NOT NULL,graph_min INTEGER NOT NULL,history_bit_end INTEGER NOT NULL,history_bit_start INTEGER NOT NULL,monitor_key INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,sort_key INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),signal_info_id INTEGER NOT NULL REFERENCES signal_infos(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_8_11_id INTEGER REFERENCES text_values(id),unknown_42_43_id INTEGER REFERENCES text_values(id),unknown_46_47_id INTEGER REFERENCES text_values(id),relation_count INTEGER NOT NULL CHECK(relation_count>=0));
INSERT INTO "native_recorder_monitors_defs" VALUES(1,7,0,255,0,0,0,0,0,7,0,1,54695,0,1,2,10,1,8,9,147,1,10,11,11,0);
CREATE TABLE native_recorder_monitors_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_monitors_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "native_recorder_monitors_entries" VALUES(1,1,4,50,0);
CREATE TABLE native_recorder_monitors_links(definition_id INTEGER NOT NULL REFERENCES native_recorder_monitors_defs(id),position INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE native_recorder_triggers_defs(id INTEGER PRIMARY KEY,dependent_data_0 INTEGER NOT NULL,dependent_data_1 INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,memo_string_index INTEGER NOT NULL,name_string_index INTEGER NOT NULL,trigger_id INTEGER NOT NULL,trigger_variant INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),memo_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_33_35_id INTEGER REFERENCES text_values(id),relation_count INTEGER NOT NULL CHECK(relation_count>=0));
INSERT INTO "native_recorder_triggers_defs" VALUES(1,500,1,0,0,73442,54881,2,0,1,12,12,13,1,3,6);
CREATE TABLE native_recorder_triggers_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_triggers_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
INSERT INTO "native_recorder_triggers_entries" VALUES(1,1,4,51,0);
CREATE TABLE native_recorder_triggers_links(definition_id INTEGER NOT NULL REFERENCES native_recorder_triggers_defs(id),position INTEGER NOT NULL,"value" INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
INSERT INTO "native_recorder_triggers_links" VALUES(1,0,15);
INSERT INTO "native_recorder_triggers_links" VALUES(1,1,16);
INSERT INTO "native_recorder_triggers_links" VALUES(1,2,1);
INSERT INTO "native_recorder_triggers_links" VALUES(1,3,0);
INSERT INTO "native_recorder_triggers_links" VALUES(1,4,0);
INSERT INTO "native_recorder_triggers_links" VALUES(1,5,0);
CREATE TABLE signal_infos(id INTEGER PRIMARY KEY);
CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT UNIQUE);
INSERT INTO "text_values" VALUES(1,'native-ddr-50');
INSERT INTO "text_values" VALUES(2,'CDbDDRAddressTable');
INSERT INTO "text_values" VALUES(3,'a5a5a5');
INSERT INTO "text_values" VALUES(4,'A_B_CAN_P4');
INSERT INTO "text_values" VALUES(5,'equal');
INSERT INTO "text_values" VALUES(6,'CDbDDRInvalidConditionTable');
INSERT INTO "text_values" VALUES(7,'a5');
INSERT INTO "text_values" VALUES(8,'Trigger ID');
INSERT INTO "text_values" VALUES(9,'CDbDDRMonitorTable');
INSERT INTO "text_values" VALUES(10,'a5a5a5a5');
INSERT INTO "text_values" VALUES(11,'a5a5');
INSERT INTO "text_values" VALUES(12,'Accelerator pedal opening angle signal is high during low speed');
INSERT INTO "text_values" VALUES(13,'CDbDDRTriggerTable');
INSERT INTO "text_values" VALUES(14,'native-ddr-110');
INSERT INTO "text_values" VALUES(15,'CDbDDRCaseInfoDataIdTable');
INSERT INTO "text_values" VALUES(16,'A_B_CAN_P5');
INSERT INTO "text_values" VALUES(17,'CDbDDRHeaderInfoDataIdTable');
COMMIT;`
const nativeRecorderSamples = `{"addresses": {"address": 0, "exception_flag": 0, "exception_id": 0, "family": "native-ddr-50", "header_member": true, "monitor_key": 1, "monitor_records": [{"record": 0, "table": 50}], "record": 0, "record_class": "CDbDDRAddressTable", "record_length": 0, "section_kind": 1, "source": "A_B_CAN_P4", "table": 52, "trigger_id": 2, "uninterpreted_bytes": {"13_15": "a5a5a5"}}, "invalid_conditions": {"combination_mode": 0, "comparison": "equal", "comparison_mode": 1, "comparison_value": 254, "condition_order": 1, "exception_flag": 0, "exception_id": 0, "family": "native-ddr-50", "record": 0, "record_class": "CDbDDRInvalidConditionTable", "referenced_signal_key": 3, "signal_key": 3, "source": "A_B_CAN_P4", "table": 53, "uninterpreted_bytes": {"15": "a5"}}, "monitors": {"bit_end": 7, "bit_start": 0, "data_max": 255, "data_min": 0, "exception_flag": 0, "exception_id": 0, "family": "native-ddr-50", "graph_max": 0, "graph_min": 0, "history_bit_end": 7, "history_bit_start": 0, "invalid_condition_records": [], "monitor_key": 1, "name": "Trigger ID", "name_string_index": 54695, "pattern_display_key": 0, "physical_data_key": 1, "presentation_mode": 2, "record": 0, "record_class": "CDbDDRMonitorTable", "signal_info": {"$signal_info": 147}, "sort_key": 10, "source": "A_B_CAN_P4", "table": 50, "uninterpreted_bytes": {"8_11": "a5a5a5a5", "42_43": "a5a5", "46_47": "a5a5"}}, "triggers": {"dependent_data_0": 500, "dependent_data_1": 1, "exception_flag": 0, "exception_id": 0, "family": "native-ddr-50", "header_parameters": [15, 16, 1, 0, 0, 0], "memo": "Accelerator pedal opening angle signal is high during low speed", "memo_string_index": 73442, "name": "Accelerator pedal opening angle signal is high during low speed", "name_string_index": 54881, "record": 0, "record_class": "CDbDDRTriggerTable", "source": "A_B_CAN_P4", "table": 51, "trigger_id": 2, "trigger_variant": 0, "uninterpreted_bytes": {"33_35": "a5a5a5"}}, "case_data_ids": {"case_did": 5377, "case_parameter": 36, "case_selector": 1, "exception_flag": 0, "exception_id": 0, "family": "native-ddr-110", "header_did": 5136, "record": 0, "record_class": "CDbDDRCaseInfoDataIdTable", "source": "A_B_CAN_P5", "table": 109, "uninterpreted_bytes": {"9": "a5", "11": "a5"}}, "header_data_ids": {"exception_flag": 0, "exception_id": 0, "family": "native-ddr-110", "header_did": 5136, "header_parameter": 62, "record": 0, "record_class": "CDbDDRHeaderInfoDataIdTable", "source": "A_B_CAN_P5", "table": 108, "uninterpreted_bytes": {"7": "a5"}}}`

func nativeRecorderTestDB(t *testing.T) *DB {
	t.Helper()
	s, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	s.SetMaxOpenConns(1)
	if _, e = s.Exec(nativeRecorderFixture); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	return &DB{db: s}
}
func TestNativeRecorderPointAndBatch(t *testing.T) {
	d := nativeRecorderTestDB(t)
	ctx := context.Background()
	var samples map[string]any
	json.Unmarshal([]byte(nativeRecorderSamples), &samples)
	nodes := []any{}
	want := []any{}
	for i := 0; i < 600; i++ {
		kind := nativeRecorderNames[i%6]
		node := map[string]any{"$native_recorder_" + kind: json.Number("1")}
		point, e := d.nativeRecorder(ctx, kind, 1)
		if e != nil {
			t.Fatal(e)
		}
		encoded, e := json.Marshal(point)
		if e != nil {
			t.Fatal(e)
		}
		var normalized any
		json.Unmarshal(encoded, &normalized)
		if !reflect.DeepEqual(normalized, samples[kind]) {
			t.Fatal("point mismatch", kind)
		}
		nodes = append(nodes, node)
		want = append(want, point)
	}
	batch, handled, e := d.nativeRecorderArray(ctx, nodes, nil)
	if !handled || e != nil {
		t.Fatal(handled, e)
	}
	if !reflect.DeepEqual(batch, want) {
		t.Fatal("batch differs from point")
	}
	// Runtime case-ID facts have ordinary numeric types; decoder references in
	// monitor rows are verified independently by the shared signal-info tests.
	runtime, e := d.runtimeValue(ctx, map[string]any{"$native_recorder_case_data_ids": json.Number("1")})
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := runtime.(map[string]any)["case_did"].(float64); !ok {
		t.Fatal("wrong runtime numeric type")
	}
}
func TestNativeRecorderMissingLink(t *testing.T) {
	d := nativeRecorderTestDB(t)
	if _, e := d.db.Exec("DELETE FROM native_recorder_addresses_links"); e != nil {
		t.Fatal(e)
	}
	nodes := []any{}
	for i := 0; i < 4; i++ {
		nodes = append(nodes, map[string]any{"$native_recorder_addresses": json.Number("1")})
	}
	if _, _, e := d.nativeRecorderArray(context.Background(), nodes, nil); e == nil {
		t.Fatal("missing relationship accepted")
	}
}
