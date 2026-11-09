package main

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
)

func readApp(t *testing.T, document map[string]any) *runtimeApp {
	t.Helper()
	c := openDocument(t, "ecu.json", document)
	p := &diagnostic.Profile{DB: c, Region: "NA", Index: map[string]any{}, Regional: map[string]any{"selected_categories": map[string]any{"405": map[string]any{"catalog_member": "ecu.json"}}}, ECUs: []diagnostic.EcuSpec{{Key: "eps", Name: "Steering", CategoryID: 405}}}
	return &runtimeApp{ctx: context.Background(), db: c, profile: p, opt: runtimeOptions{values: map[string][]string{}, flags: map[string]bool{}, json: true}}
}
func TestNativeDecodeUsesSQLiteAndKeepsUnknownRaw(t *testing.T) {
	signal := map[string]any{"name": "Steering Angle", "decoder": "p5-linear-msb0-v1", "bit_start": 0, "bit_end": 15, "signed": true, "mul": 15, "div": 1, "offset": 0, "decimal_point_count": 1, "unit": "deg"}
	unknown := map[string]any{"name": "Unknown", "decoder": "unrecovered", "bit_start": 0, "bit_end": 15}
	r := readApp(t, map[string]any{"dids": map[string]any{"0x1037": []any{signal, unknown}}})
	out, e := capture(func() error { return r.didCommand([]string{"did", "decode", "eps", "0x1037", "0001"}) })
	if e != nil {
		t.Fatal(e)
	}
	var result map[string]any
	if e = json.Unmarshal([]byte(out), &result); e != nil {
		t.Fatal(e)
	}
	rows := runtimeRows(result["signals"])
	if result["data_hex"] != "0001" || rows[0]["value"] != "1.5" || rows[1]["error"] == nil || r.transport != nil {
		t.Fatalf("%s", out)
	}
}
func TestMonitorTermsExpandWithoutDuplicateDIDs(t *testing.T) {
	r := readApp(t, map[string]any{"dids": map[string]any{"0x1001": []any{map[string]any{"name": "LTA Main"}}, "0x1002": []any{map[string]any{"name": "LTA State"}}}})
	q, e := r.didQueries(r.profile.ECUs[0], []string{"LTA", "0x1001"}, true)
	if e != nil || len(q) != 2 {
		t.Fatalf("%+v %v", q, e)
	}
	if _, e = r.didQueries(r.profile.ECUs[0], []string{"LTA"}, false); e == nil {
		t.Fatal("ambiguous exact read accepted")
	}
}
func TestRuntimeParserRejectsUnknownAndMissingOptions(t *testing.T) {
	for _, args := range [][]string{{"did", "read", "eps", "--interval"}, {"--force=true"}, {"--registrry", "old.json"}} {
		if _, _, e := parseRuntime(args); e == nil {
			t.Fatalf("accepted %v", args)
		}
	}
}
func TestRawAndDTCStatusDoNotNeedCatalog(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/missing/catalog.sqlite")
	out, e := capture(func() error { return run([]string{"dtc", "decode", "0x09", "--json"}) })
	if e != nil || !strings.Contains(out, "CONFIRMED_DTC") {
		t.Fatalf("%s %v", out, e)
	}
	if e = run([]string{"uds", "raw", "0x7e0", "0x14"}); e == nil || !strings.Contains(e.Error(), "--force") {
		t.Fatalf("%v", e)
	}
}
func TestRuntimeSupportUsesP5StaticGeneration(t *testing.T) {
	r := readApp(t, map[string]any{"active_tests": []any{map[string]any{"routine_id": 4097}}})
	r.profile.ECUs[0].Generation = 0x15
	meta := runtimeMap(runtimeMap(r.profile.Regional["selected_categories"])["405"])
	meta["support_family"] = "p5"
	meta["support_mode"] = "p5-mazda"
	r.profile.Index["support_contracts"] = map[string]any{"p5": map[string]any{"routine_root": map[string]any{"request": "31011001", "positive_sid": "71"}}}
	support, e := r.support(r.profile.ECUs[0], nil, true)
	if e != nil {
		t.Fatal(e)
	}
	ids, e := support.SupportedIDs(context.Background())
	if e != nil || len(ids) != 1 || ids[0] != 4097 {
		t.Fatalf("%v %v", ids, e)
	}
}

func TestDTCConnectionFailureAndUnresolvedRoutes(t *testing.T) {
	for _, action := range []string{"scan", "clear"} {
		for _, scenario := range []string{"connection", "unresolved", "empty"} {
			t.Run(action+"/"+scenario, func(t *testing.T) {
				r := readApp(t, map[string]any{})
				r.profile.Vehicle = map[string]any{"vehicle_type": 1}
				r.opt.values["transport"] = []string{"definitely-invalid"}
				if scenario == "connection" {
					r.profile.ECUs[0].RouteResolved = true
					r.profile.ECUs[0].Address = 0x7e0
				}
				if scenario == "empty" {
					r.profile.ECUs = nil
				}
				e := r.dtcCommand([]string{"dtc", action})
				want := "no selected ECU"
				if scenario == "connection" {
					want = "unknown transport"
				}
				if e == nil || !strings.Contains(e.Error(), want) {
					t.Fatalf("%v", e)
				}
				if r.transport != nil {
					t.Fatal("unexpected transport")
				}
			})
		}
	}
}

type disconnectedCAN struct{ rawFakeCAN }

func (f *disconnectedCAN) Send(context.Context, transport.Frame) error {
	return errors.New("adapter disconnected")
}
func TestDTCDoesNotClearAfterFailedScan(t *testing.T) {
	for _, failure := range []string{"disconnect", "negative"} {
		t.Run(failure, func(t *testing.T) {
			r := readApp(t, map[string]any{})
			r.profile.Vehicle = map[string]any{"vehicle_type": 1}
			r.profile.ECUs[0].Address = 0x7e0
			r.profile.ECUs[0].RouteResolved = true
			fake := &rawFakeCAN{reply: []transport.Frame{{Address: 0x7e8, Data: []byte{3, 0x7f, 0x19, 0x11}, Bus: 0}}}
			r.transport = fake
			if failure == "disconnect" {
				r.transport = &disconnectedCAN{}
			}
			_, e := capture(func() error { return r.dtcCommand([]string{"dtc", "clear"}) })
			if e == nil {
				t.Fatal("failed scan succeeded")
			}
			if failure == "disconnect" && !strings.Contains(e.Error(), "adapter disconnected") {
				t.Fatal(e)
			}
			for _, frame := range fake.sent {
				if frame.Address == 0x7df || len(frame.Data) > 1 && frame.Data[1] == 0x14 {
					t.Fatalf("clear transmitted after failed scan: %v", frame)
				}
			}
		})
	}
}

func TestFunctionalClearCompletenessRequiresExpectedMetadata(t *testing.T) {
	complete, status := functionalClearCompleteness(nil, nil)
	if complete || status != "unknown" {
		t.Fatalf("empty expected claimed %v %s", complete, status)
	}
	complete, status = functionalClearCompleteness(map[uint32]bool{}, map[uint32]bool{})
	if complete || status != "unknown" {
		t.Fatalf("zero expected claimed complete: %v %s", complete, status)
	}
	complete, status = functionalClearCompleteness(map[uint32]bool{0x7e8: true}, map[uint32]bool{})
	if complete || status != "incomplete" {
		t.Fatalf("missing ACK: %v %s", complete, status)
	}
	complete, status = functionalClearCompleteness(map[uint32]bool{0x7e8: true}, map[uint32]bool{0x7e8: true})
	if !complete || status != "complete" {
		t.Fatalf("matching ACK: %v %s", complete, status)
	}
}

func TestDTCClearDoesNotClaimFunctionalCompletionWithoutLegislatedMetadata(t *testing.T) {
	r := readApp(t, map[string]any{"dtcs": map[string]any{}})
	r.profile.Vehicle = map[string]any{"vehicle_type": 1}
	r.profile.ECUs[0].Address = 0x7e0
	r.profile.ECUs[0].RouteResolved = true
	r.profile.ECUs[0].Route = map[string]any{"physical_request_address": 0x7e0}
	r.opt.json = true
	bus := &sessionCAN{reply: func(req []byte) ([]byte, error) {
		switch req[0] {
		case 0x19:
			return []byte{0x59, 0x02}, nil
		case 0x14:
			return []byte{0x54}, nil
		case 0x10:
			return []byte{0x50, req[1]}, nil
		case 0x04:
			return nil, nil
		default:
			return nil, errors.New("unexpected SID")
		}
	}}
	r.transport = bus
	out, err := capture(func() error { return r.dtcCommand([]string{"dtc", "clear"}) })
	if err != nil && !strings.Contains(out, "functional_completeness") {
		// clear may exit 0; if it errors for another reason, show it
	}
	if !strings.Contains(out, `"functional_completeness": "unknown"`) && !strings.Contains(out, `"functional_completeness":"unknown"`) {
		t.Fatalf("want unknown functional completeness, got %s err=%v", out, err)
	}
	if strings.Contains(out, `"functional_complete": true`) || strings.Contains(out, `"functional_complete":true`) {
		t.Fatalf("empty legislated metadata claimed functional completion: %s", out)
	}
}
