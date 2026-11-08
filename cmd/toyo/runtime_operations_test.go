package main

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"path/filepath"
	"strings"
	"testing"
)

func operationApp(t *testing.T, row map[string]any) *runtimeApp {
	return operationDocumentApp(t, map[string]any{"active_tests": []any{row}})
}
func operationDocumentApp(t *testing.T, doc map[string]any) *runtimeApp {
	t.Helper()
	path := filepath.Join(t.TempDir(), "operations.sqlite")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	_, e = db.Exec("CREATE TABLE meta(key TEXT,value TEXT); INSERT INTO meta VALUES ('schema','toyota-diagnostics-sqlite-v2'); CREATE TABLE runtime_documents(member TEXT PRIMARY KEY,data BLOB); CREATE TABLE item_sources(item_id INTEGER PRIMARY KEY,member TEXT,pointer TEXT); CREATE TABLE item_payloads(item_id INTEGER PRIMARY KEY,data BLOB)")
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(doc)
	var packed bytes.Buffer
	w := zlib.NewWriter(&packed)
	w.Write(raw)
	w.Close()
	_, e = db.Exec("INSERT INTO runtime_documents VALUES ('category.json',?)", packed.Bytes())
	if e != nil {
		t.Fatal(e)
	}
	db.Close()
	cat, e := catalog.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { cat.Close() })
	p := &diagnostic.Profile{DB: cat, Index: map[string]any{}, Regional: map[string]any{"selected_categories": map[string]any{"1": map[string]any{"catalog_member": "category.json"}}}, ECUs: []diagnostic.EcuSpec{{Key: "ecu", CategoryID: 1}}}
	return &runtimeApp{ctx: context.Background(), db: cat, profile: p, opt: runtimeOptions{values: map[string][]string{}, flags: map[string]bool{}}}
}
func opRoutineFixture() map[string]any {
	return map[string]any{"id": 1, "name": "Routine", "kind": "routine", "execution": "executable", "service": 0x31, "positive_response": 0x71, "session_requirement": "none", "fixed_request": true, "routine_id": 0x1001, "start_static": "31011001", "stop_static": "31021001"}
}
func TestOperationDryRunNeedsNoTransport(t *testing.T) {
	r := operationApp(t, opRoutineFixture())
	out, e := capture(func() error { return r.operations([]string{"active-test", "run", "ecu", "1"}) })
	if e != nil || !strings.Contains(out, "Plan only. No requests sent.") || r.transport != nil {
		t.Fatalf("%s %v", out, e)
	}
}
func TestOperationRejectsWrongValueFlagsBeforeTransport(t *testing.T) {
	for _, key := range []string{"raw-value", "engineering-value", "choice", "value", "button"} {
		t.Run(key, func(t *testing.T) {
			r := operationApp(t, opRoutineFixture())
			r.opt.flags["execute"] = true
			r.opt.values[key] = []string{"01"}
			e := r.operations([]string{"active-test", "run", "ecu", "1"})
			if e == nil || r.transport != nil {
				t.Fatal(e)
			}
			if strings.Contains(e.Error(), "Panda") || strings.Contains(e.Error(), "VIN") {
				t.Fatalf("transport attempted before validation: %v", e)
			}
		})
	}
}
func TestOperationRefusesStaticPlanBeforeTransport(t *testing.T) {
	row := opRoutineFixture()
	row["stop_static"] = ""
	r := operationApp(t, row)
	r.opt.flags["execute"] = true
	e := r.operations([]string{"active-test", "run", "ecu", "1"})
	if e == nil || !strings.Contains(e.Error(), "not executable") || r.transport != nil {
		t.Fatal(e)
	}
}

func opDirectFixture(id int) map[string]any {
	return map[string]any{"id": id, "name": "Direct", "kind": "direct", "execution": "plan_only", "service": 0x2f, "positive_response": 0x6f, "session_requirement": "none", "did": 0x2801, "start_prefix": "2f280103", "stop_prefix": "2f280100", "runtime_length_minimum": 1, "runtime_length_probe": map[string]any{"kind": "read_data_by_identifier_value_length", "selector": "0xCA", "request": "222801", "check": "62", "response_prefix_length": 3}, "encoding_mode": 1, "bit_start": 0, "bit_end": 0, "control_enable_mask": map[string]any{"start": "selected_bit_range_or_alternate", "stop": "selected_bit_range_or_alternate"}}
}
func opGroupFixture() map[string]any {
	return map[string]any{"active_tests": []any{opDirectFixture(1), opDirectFixture(2)}, "active_test_groups": map[string]any{"groups": []any{map[string]any{"group_id": 9, "name": "Pair", "execution": "materializable", "composer": "or_member_start_stop_frames", "did": 0x2801, "member_inputs": []any{map[string]any{"active_test_id": 1, "input_slot": 1, "did": 0x2801}, map[string]any{"active_test_id": 2, "input_slot": 2, "did": 0x2801}}}}}}
}
func TestGroupInputsRejectedBeforeVehicleOrTransport(t *testing.T) {
	cases := []struct {
		name   string
		values []string
		want   string
	}{
		{"malformed", []string{"garbage"}, "requires MEMBER_ID=value"},
		{"duplicate", []string{"1=0", "1=1"}, "duplicate member"},
		{"unknown", []string{"3=0"}, "unknown member"},
		{"missing", []string{"1=0"}, "missing value"},
		{"no values", nil, "missing value"},
		{"range", []string{"1=2", "2=0"}, "does not fit"},
		{"invalid scalar", []string{"1=nope", "2=0"}, "invalid syntax"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := operationDocumentApp(t, opGroupFixture())
			r.opt.flags["execute"] = true
			r.opt.values["member-raw-value"] = tc.values
			e := r.operations([]string{"active-test", "group-run", "ecu", "9"})
			if e == nil || !strings.Contains(e.Error(), tc.want) || r.transport != nil || len(r.sessions) > 0 {
				t.Fatalf("expected %q before transport, got %v", tc.want, e)
			}
		})
	}
}
func TestUtilityBytesRejectedBeforeVehicleOrTransport(t *testing.T) {
	for _, key := range []string{"value", "mask", "option-record"} {
		t.Run(key, func(t *testing.T) {
			row := opDirectFixture(1)
			row["execution"] = "executable"
			row["runtime_length"] = 1
			r := operationDocumentApp(t, map[string]any{"utilities": []any{row}})
			r.opt.flags["execute"] = true
			r.opt.values[key] = []string{"zz"}
			e := r.operations([]string{"utility", "run", "ecu", "1"})
			if e == nil || !strings.Contains(e.Error(), "--"+key) || r.transport != nil || len(r.sessions) > 0 {
				t.Fatal(e)
			}
		})
	}
}
func TestUtilityShapeRejectedBeforeVehicleOrTransport(t *testing.T) {
	cases := []struct{ name, kind, key, value, want string }{{"direct length", "direct", "value", "0000", "exactly 1"}, {"fixed option", "routine", "option-record", "00", "fixed utility"}, {"routine mask", "routine", "mask", "01", "not --value or --mask"}}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			row := opRoutineFixture()
			if tc.kind == "direct" {
				row = opDirectFixture(1)
				row["execution"] = "executable"
				row["runtime_length"] = 1
			}
			r := operationDocumentApp(t, map[string]any{"utilities": []any{row}})
			r.opt.flags["execute"] = true
			r.opt.values[tc.key] = []string{tc.value}
			e := r.operations([]string{"utility", "run", "ecu", "1"})
			if e == nil || !strings.Contains(e.Error(), tc.want) || r.transport != nil || len(r.sessions) > 0 {
				t.Fatal(e)
			}
		})
	}
}
