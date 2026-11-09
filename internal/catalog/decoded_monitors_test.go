package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"testing"
)

const decodedFixture = `BEGIN TRANSACTION;
CREATE TABLE decoded_behavior(id INTEGER PRIMARY KEY,"behavior_code" INTEGER NOT NULL,"comment_string_index" INTEGER NOT NULL,"name_string_index" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,comment_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),signature_id INTEGER REFERENCES text_values(id),source_id INTEGER REFERENCES text_values(id),unassigned_tail_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_behavior" VALUES(1,1280,0,95820,0,87,NULL,15,16,1,17);
CREATE TABLE decoded_condition(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"comparison_mode" INTEGER NOT NULL,"comparison_value" INTEGER NOT NULL,"condition_type" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"referenced_did" INTEGER NOT NULL,"unassigned_tail" INTEGER NOT NULL,"variable_id" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),variable_bytes_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_condition" VALUES(1,0,0,0,0,7,0,0,1,0,63,7936,0,11278,1,13);
CREATE TABLE decoded_condition_compact(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"condition_type" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"referenced_did" INTEGER NOT NULL,"variable_id" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),variable_bytes_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_condition_compact" VALUES(1,7,7,7,0,0,1,0,80,1027,11290,1,14);
CREATE TABLE decoded_data_id(id INTEGER PRIMARY KEY,"did" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"fixed_length" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"support_mode" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_data_id" VALUES(1,257,0,0,0,0,61,1,1);
CREATE TABLE decoded_dtc_status(id INTEGER PRIMARY KEY,"comparison_value" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"status_mask" INTEGER NOT NULL,"unassigned_fields__byte11" INTEGER NOT NULL,"unassigned_fields__word4" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_dtc_status" VALUES(1,1,0,0,1,0,66,1,0,1,1);
CREATE TABLE decoded_dynamic_did(id INTEGER PRIMARY KEY,"did" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"maximum_payload_length" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"unassigned_tail" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_dynamic_did" VALUES(1,62209,0,0,40,0,81,0,1);
CREATE TABLE decoded_monitor(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"data_range_max" INTEGER NOT NULL,"data_range_min" INTEGER NOT NULL,"extraction_mode" INTEGER NOT NULL,"ffd_support_condition_key" INTEGER NOT NULL,"flags" INTEGER NOT NULL,"graph_max" INTEGER NOT NULL,"graph_min" INTEGER NOT NULL,"live_support_condition_key" INTEGER NOT NULL,"monitor_key" INTEGER NOT NULL,"name_string_index" INTEGER NOT NULL,"pattern_display_key" INTEGER NOT NULL,"physical_data_key" INTEGER NOT NULL,"primary_did" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"snapshot_did" INTEGER NOT NULL,"sort_key" INTEGER NOT NULL,"visibility_override" INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),source_id INTEGER REFERENCES text_values(id),unassigned_ranges__10_27_id INTEGER REFERENCES text_values(id),unassigned_ranges__2c_2f_id INTEGER REFERENCES text_values(id),unassigned_ranges__44_45_id INTEGER REFERENCES text_values(id),unassigned_ranges__4c_4f_id INTEGER REFERENCES text_values(id),physical_max REAL NOT NULL,physical_min REAL NOT NULL,pattern_records_count INTEGER NOT NULL CHECK(pattern_records_count>=0),physical_records_count INTEGER NOT NULL CHECK(physical_records_count>=0),unit_records_count INTEGER NOT NULL CHECK(unit_records_count>=0));
INSERT INTO "decoded_monitor" VALUES(1,15,15,1,0,1,0,67,1,0,0,1,66295,28,3,5631,0,62,13823,6180,0,8,1,9,10,6,4,1.0,0.0,2,1,1);
CREATE TABLE decoded_monitor_pattern_records(owner_id INTEGER NOT NULL REFERENCES decoded_monitor(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_pattern(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
INSERT INTO "decoded_monitor_pattern_records" VALUES(1,0,1);
INSERT INTO "decoded_monitor_pattern_records" VALUES(1,1,2);
CREATE TABLE decoded_monitor_physical_records(owner_id INTEGER NOT NULL REFERENCES decoded_monitor(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_physical(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
INSERT INTO "decoded_monitor_physical_records" VALUES(1,0,1);
CREATE TABLE decoded_monitor_unit_records(owner_id INTEGER NOT NULL REFERENCES decoded_monitor(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_unit(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
INSERT INTO "decoded_monitor_unit_records" VALUES(1,0,1);
CREATE TABLE decoded_pattern(id INTEGER PRIMARY KEY,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),unassigned_fields__14_15_id INTEGER REFERENCES text_values(id),unassigned_fields__18_22_id INTEGER REFERENCES text_values(id),unassigned_fields__8_11_id INTEGER REFERENCES text_values(id),value_id INTEGER NOT NULL REFERENCES freeze_patterns(id));
INSERT INTO "decoded_pattern" VALUES(1,0,0,28,1,2,3,4,1);
INSERT INTO "decoded_pattern" VALUES(2,0,0,28,1,11,3,4,2);
CREATE TABLE decoded_pcs_ffd(id INTEGER PRIMARY KEY,"pid" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"request__function_selector" INTEGER NOT NULL,"request__pid_byte_offset" INTEGER NOT NULL,"retrieval_group" INTEGER NOT NULL,"unassigned_word0" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_pcs_ffd" VALUES(1,129,0,86,17,3,1,1,7);
CREATE TABLE decoded_physical(id INTEGER PRIMARY KEY,"decimal_point_count" INTEGER NOT NULL,"div" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"mul" INTEGER NOT NULL,"offset" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"sign_mode" INTEGER NOT NULL,"unit_key" INTEGER NOT NULL,"unassigned_fields__23" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),unassigned_fields__18_19_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_physical" VALUES(1,0,1,0,0,3,1,0,2,13,0,0,0,1,6);
CREATE TABLE decoded_rob_signal(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"did" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"extraction_mode" INTEGER NOT NULL,"name_string_index" INTEGER NOT NULL,"pattern_display_key" INTEGER NOT NULL,"physical_data_key" INTEGER NOT NULL,"presentation_mode" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"record_key" INTEGER NOT NULL,"sort_key" INTEGER NOT NULL,"support_condition_key" INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),source_id INTEGER REFERENCES text_values(id),unassigned_ranges__0_1f_id INTEGER REFERENCES text_values(id),unassigned_ranges__24_2b_id INTEGER REFERENCES text_values(id),unassigned_ranges__40_41_id INTEGER REFERENCES text_values(id),unassigned_ranges__43_47_id INTEGER REFERENCES text_values(id),pattern_records_count INTEGER NOT NULL CHECK(pattern_records_count>=0),physical_records_count INTEGER NOT NULL CHECK(physical_records_count>=0),unit_records_count INTEGER NOT NULL CHECK(unit_records_count>=0));
INSERT INTO "decoded_rob_signal" VALUES(1,31,8,1282,0,0,2,95989,0,3,0,0,88,1,1,0,18,1,19,20,6,21,0,1,1);
CREATE TABLE decoded_rob_signal_pattern_records(owner_id INTEGER NOT NULL REFERENCES decoded_rob_signal(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_pattern(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
CREATE TABLE decoded_rob_signal_physical_records(owner_id INTEGER NOT NULL REFERENCES decoded_rob_signal(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_physical(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
INSERT INTO "decoded_rob_signal_physical_records" VALUES(1,0,1);
CREATE TABLE decoded_rob_signal_unit_records(owner_id INTEGER NOT NULL REFERENCES decoded_rob_signal(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_unit(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
INSERT INTO "decoded_rob_signal_unit_records" VALUES(1,0,1);
CREATE TABLE decoded_rob_special(id INTEGER PRIMARY KEY,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"signal_key" INTEGER NOT NULL,"unassigned_fields__word6" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),unassigned_fields__tail9_id INTEGER REFERENCES text_values(id));
INSERT INTO "decoded_rob_special" VALUES(1,0,0,1,0,91,9900,1,1,22);
CREATE TABLE decoded_unit(id INTEGER PRIMARY KEY,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"unassigned_fields__11" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),value_id INTEGER NOT NULL REFERENCES freeze_units(id));
INSERT INTO "decoded_unit" VALUES(1,0,0,0,0,1,1);
CREATE TABLE freeze_pattern_lists(id INTEGER PRIMARY KEY,entry_count INTEGER NOT NULL CHECK(entry_count>=0));
CREATE TABLE freeze_pattern_members(list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id),position INTEGER NOT NULL,pattern_id INTEGER NOT NULL REFERENCES freeze_patterns(id),PRIMARY KEY(list_id,position)) WITHOUT ROWID;
CREATE TABLE freeze_patterns(id INTEGER PRIMARY KEY,record INTEGER NOT NULL,string_index INTEGER NOT NULL,table_type INTEGER NOT NULL,text_id INTEGER REFERENCES text_values(id),value INTEGER NOT NULL);
INSERT INTO "freeze_patterns" VALUES(1,58,28783,14,5,0);
INSERT INTO "freeze_patterns" VALUES(2,59,28784,14,12,1);
CREATE TABLE freeze_physical_defs(id INTEGER PRIMARY KEY,bit_width INTEGER NOT NULL,decimal_point_count INTEGER NOT NULL,div INTEGER NOT NULL,mul INTEGER NOT NULL,offset INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,record INTEGER NOT NULL,sign_mode INTEGER NOT NULL,table_type INTEGER NOT NULL,unit_key INTEGER NOT NULL,unit_id INTEGER NOT NULL REFERENCES freeze_units(id),pattern_list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id));
CREATE TABLE freeze_units(id INTEGER PRIMARY KEY,genre_id INTEGER NOT NULL,record INTEGER NOT NULL,string_index INTEGER NOT NULL,table_type INTEGER NOT NULL,text_id INTEGER REFERENCES text_values(id));
INSERT INTO "freeze_units" VALUES(1,0,0,0,15,NULL);
CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT UNIQUE);
INSERT INTO "text_values" VALUES(1,'Engine_P5.ddb');
INSERT INTO "text_values" VALUES(2,'0100');
INSERT INTO "text_values" VALUES(3,'0000000001');
INSERT INTO "text_values" VALUES(4,'00000000');
INSERT INTO "text_values" VALUES(5,'OFF');
INSERT INTO "text_values" VALUES(6,'0000');
INSERT INTO "text_values" VALUES(7,'PCS2_P4.ddb');
INSERT INTO "text_values" VALUES(8,'Immobiliser Communication');
INSERT INTO "text_values" VALUES(9,'000000000100000000000000010000000000000000000000');
INSERT INTO "text_values" VALUES(10,'22bd0000');
INSERT INTO "text_values" VALUES(11,'0200');
INSERT INTO "text_values" VALUES(12,'ON');
INSERT INTO "text_values" VALUES(13,'1f00');
INSERT INTO "text_values" VALUES(14,'0403');
INSERT INTO "text_values" VALUES(15,'Shift P Operation during Running');
INSERT INTO "text_values" VALUES(16,'X0500');
INSERT INTO "text_values" VALUES(17,'000000010100');
INSERT INTO "text_values" VALUES(18,'Total Distance Traveled');
INSERT INTO "text_values" VALUES(19,'0000000000000000000000000000000000000000000000000000000000000000');
INSERT INTO "text_values" VALUES(20,'a215010000000000');
INSERT INTO "text_values" VALUES(21,'0000000000');
INSERT INTO "text_values" VALUES(22,'000000');
COMMIT;`
const decodedSamples = `{"pattern": {"exception_handler_flag": 0, "exception_handler_id": 0, "key": 28, "name": "OFF", "name_string_index": 28783, "record": 58, "source": "Engine_P5.ddb", "table": 14, "unassigned_fields": {"14_15": "0100", "18_22": "0000000001", "8_11": "00000000"}, "value": 0}, "physical": {"decimal_point_count": 0, "div": 1, "exception_handler_flag": 0, "exception_handler_id": 0, "key": 3, "mul": 1, "offset": 0, "record": 2, "sign_mode": 0, "source": "Engine_P5.ddb", "table": 13, "unassigned_fields": {"18_19": "0000", "23": 0}, "unit_key": 0}, "unit": {"exception_handler_flag": 0, "exception_handler_id": 0, "genre_id": 0, "key": 0, "name": null, "name_string_index": 0, "record": 0, "source": "Engine_P5.ddb", "table": 15, "unassigned_fields": {"11": 0}}, "pcs_ffd": {"kind": "pcs_operation_ffd_pid", "pid": 129, "record": 0, "request": {"function_selector": 17, "pid_byte_offset": 3}, "retrieval_group": 1, "source": "PCS2_P4.ddb", "table": 86, "unassigned_word0": 1}, "data_id": {"did": 257, "exception_handler_flag": 0, "exception_handler_id": 0, "fixed_length": 0, "kind": "data_id", "record": 0, "source": "Engine_P5.ddb", "support_mode": 1, "table": 61}, "monitor": {"bit_end": 15, "bit_start": 15, "data_range_max": 1, "data_range_min": 0, "extraction_mode": 1, "ffd_support_condition_key": 0, "flags": 67, "graph_max": 1, "graph_min": 0, "kind": "monitor", "live_support_condition_key": 0, "monitor_key": 1, "name": "Immobiliser Communication", "name_string_index": 66295, "pattern_display_key": 28, "pattern_records": [{"exception_handler_flag": 0, "exception_handler_id": 0, "key": 28, "name": "OFF", "name_string_index": 28783, "record": 58, "source": "Engine_P5.ddb", "table": 14, "unassigned_fields": {"14_15": "0100", "18_22": "0000000001", "8_11": "00000000"}, "value": 0}, {"exception_handler_flag": 0, "exception_handler_id": 0, "key": 28, "name": "ON", "name_string_index": 28784, "record": 59, "source": "Engine_P5.ddb", "table": 14, "unassigned_fields": {"14_15": "0200", "18_22": "0000000001", "8_11": "00000000"}, "value": 1}], "physical_data_key": 3, "physical_max": 1.0, "physical_min": 0.0, "physical_records": [{"decimal_point_count": 0, "div": 1, "exception_handler_flag": 0, "exception_handler_id": 0, "key": 3, "mul": 1, "offset": 0, "record": 2, "sign_mode": 0, "source": "Engine_P5.ddb", "table": 13, "unassigned_fields": {"18_19": "0000", "23": 0}, "unit_key": 0}], "primary_did": 5631, "record": 0, "snapshot_did": 13823, "sort_key": 6180, "source": "Engine_P5.ddb", "table": 62, "unassigned_ranges": {"10_27": "000000000100000000000000010000000000000000000000", "2c_2f": "22bd0000", "44_45": "0000", "4c_4f": "00000000"}, "unit_records": [{"exception_handler_flag": 0, "exception_handler_id": 0, "genre_id": 0, "key": 0, "name": null, "name_string_index": 0, "record": 0, "source": "Engine_P5.ddb", "table": 15, "unassigned_fields": {"11": 0}}], "visibility_override": 0}, "condition": {"bit_end": 0, "bit_start": 0, "comparison_mode": 0, "comparison_value": 0, "condition_type": 7, "exception_handler_flag": 0, "exception_handler_id": 0, "key": 1, "kind": "support_condition", "record": 0, "referenced_did": 7936, "source": "Engine_P5.ddb", "table": 63, "unassigned_tail": 0, "variable_bytes": "1f00", "variable_id": 11278}, "condition_compact": {"bit_end": 7, "bit_start": 7, "condition_type": 7, "exception_handler_flag": 0, "exception_handler_id": 0, "key": 1, "kind": "support_condition", "record": 0, "referenced_did": 1027, "source": "Engine_P5.ddb", "table": 80, "variable_bytes": "0403", "variable_id": 11290}, "dtc_status": {"comparison_value": 1, "exception_handler_flag": 0, "exception_handler_id": 0, "key": 1, "kind": "dtc_status", "record": 0, "source": "Engine_P5.ddb", "status_mask": 1, "table": 66, "unassigned_fields": {"byte11": 0, "word4": 1}}, "dynamic_did": {"did": 62209, "exception_handler_flag": 0, "exception_handler_id": 0, "kind": "dynamic_did", "maximum_payload_length": 40, "record": 0, "source": "Engine_P5.ddb", "table": 81, "unassigned_tail": 0}, "behavior": {"behavior_code": 1280, "comment": null, "comment_string_index": 0, "kind": "behavior_code", "name": "Shift P Operation during Running", "name_string_index": 95820, "record": 0, "signature": "X0500", "source": "Engine_P5.ddb", "table": 87, "unassigned_tail": "000000010100"}, "rob_signal": {"bit_end": 31, "bit_start": 8, "did": 1282, "exception_handler_flag": 0, "exception_handler_id": 0, "extraction_mode": 2, "kind": "rob_signal", "name": "Total Distance Traveled", "name_string_index": 95989, "pattern_display_key": 0, "pattern_records": [], "physical_data_key": 3, "physical_records": [{"decimal_point_count": 0, "div": 1, "exception_handler_flag": 0, "exception_handler_id": 0, "key": 3, "mul": 1, "offset": 0, "record": 2, "sign_mode": 0, "source": "Engine_P5.ddb", "table": 13, "unassigned_fields": {"18_19": "0000", "23": 0}, "unit_key": 0}], "presentation_mode": 0, "record": 0, "record_key": 1, "sort_key": 1, "source": "Engine_P5.ddb", "support_condition_key": 0, "table": 88, "unassigned_ranges": {"0_1f": "0000000000000000000000000000000000000000000000000000000000000000", "24_2b": "a215010000000000", "40_41": "0000", "43_47": "0000000000"}, "unit_records": [{"exception_handler_flag": 0, "exception_handler_id": 0, "genre_id": 0, "key": 0, "name": null, "name_string_index": 0, "record": 0, "source": "Engine_P5.ddb", "table": 15, "unassigned_fields": {"11": 0}}]}, "rob_special": {"exception_handler_flag": 0, "exception_handler_id": 0, "key": 1, "kind": "rob_special_signal", "record": 0, "signal_key": 9900, "source": "Engine_P5.ddb", "table": 91, "unassigned_fields": {"tail9": "000000", "word6": 1}}}`

func TestDecodedMonitorDomains(t *testing.T) {
	s, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.SetMaxOpenConns(1)
	if _, e = s.Exec(decodedFixture); e != nil {
		t.Fatal(e)
	}
	d := &DB{db: s}
	ids := map[string]string{"pattern": "1", "physical": "1", "unit": "1", "pcs_ffd": "1", "data_id": "1", "monitor": "1", "condition": "1", "condition_compact": "1", "dtc_status": "1", "dynamic_did": "1", "behavior": "1", "rob_signal": "1", "rob_special": "1"}
	var samples map[string]any
	json.Unmarshal([]byte(decodedSamples), &samples)
	for name, want := range samples {
		got, e := d.runtimeValue(context.Background(), map[string]any{"$decoded_" + name: json.Number(ids[name])})
		if e != nil {
			t.Fatal(name, e)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s mismatch: %#v", name, got)
		}
	}
}
func TestDecodedMonitorMissingChild(t *testing.T) {
	s, e := sql.Open("sqlite", ":memory:")
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	s.SetMaxOpenConns(1)
	if _, e = s.Exec(decodedFixture); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Exec("DELETE FROM decoded_monitor_pattern_records WHERE position=0"); e != nil {
		t.Fatal(e)
	}
	d := &DB{db: s}
	if _, e = d.decodedMonitor(context.Background(), "monitor", 1); e == nil {
		t.Fatal("missing relation accepted")
	}
}
