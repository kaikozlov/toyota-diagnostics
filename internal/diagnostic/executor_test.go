package diagnostic

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

func routineRow() map[string]any {
	return map[string]any{"id": 0x2001, "kind": "routine", "execution": "executable", "service": 0x31, "positive_response": 0x71, "session_requirement": "default", "fixed_request": true, "routine_id": 0x1105, "start_static": "31011105", "stop_static": "31021105", "result_static": "31031105"}
}
func directRow() map[string]any {
	return map[string]any{"id": 0x1001, "kind": "direct", "execution": "executable", "service": 0x2f, "positive_response": 0x6f, "session_requirement": "none", "did": 0x2801, "start_prefix": "2f280103", "stop_prefix": "2f280100", "runtime_length": 2, "encoding_mode": 1, "bit_start": 15, "bit_end": 15, "control_enable_mask": map[string]any{"start": "none", "stop": "selected_bit_range"}}
}
func TestPlanRefusesIncompleteGeometry(t *testing.T) {
	for key, value := range map[string]any{"service": 0x2f, "positive_response": 0x70, "session_requirement": nil, "routine_id": 65535, "stop_static": "2e021105", "execution": "plan_only"} {
		r := routineRow()
		r[key] = value
		if ResolvePlan(r).Executable() {
			t.Fatalf("accepted %s=%v", key, value)
		}
	}
}
func TestPlanOnlyNeverTransmits(t *testing.T) {
	c := &scriptedClient{}
	p := ResolvePlan(routineRow())
	r, e := RunTest(context.Background(), NewSession(c, nil), p, RunOptions{Hold: time.Second})
	if e != nil || r.Executed || len(c.calls) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
func TestRoutineStartPollStop(t *testing.T) {
	c := &scriptedClient{}
	p := ResolvePlan(routineRow())
	r, e := RunTest(context.Background(), NewSession(c, nil), p, RunOptions{Execute: true, Hold: 5 * time.Millisecond, PollInterval: time.Millisecond})
	if e != nil {
		t.Fatal(e)
	}
	if !r.Executed || len(r.Statuses) == 0 || !bytes.Equal(c.calls[0], p.Start) || !bytes.Equal(c.calls[len(c.calls)-1], p.Stop) {
		t.Fatal(c.calls)
	}
}
func TestOperationErrorPreservesOriginalAndStops(t *testing.T) {
	sentinel := errors.New("poll failed")
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) {
		if q[0] == 0x31 && q[1] == 3 {
			return nil, sentinel
		}
		return q[1:], nil
	}}
	p := ResolvePlan(routineRow())
	_, e := RunTest(context.Background(), NewSession(c, nil), p, RunOptions{Execute: true, Hold: time.Second, PollInterval: time.Millisecond})
	if !errors.Is(e, sentinel) {
		t.Fatal(e)
	}
	if !bytes.Equal(c.calls[len(c.calls)-1], p.Stop) {
		t.Fatal(c.calls)
	}
}
func TestDirectMaterializationUsesExactProbe(t *testing.T) {
	row := directRow()
	delete(row, "runtime_length")
	row["execution"] = "plan_only"
	row["runtime_length_minimum"] = 2
	row["runtime_length_probe"] = map[string]any{"kind": "read_data_by_identifier_value_length", "selector": "0xCA", "request": "222801", "check": "62", "response_prefix_length": 3}
	p := ResolvePlan(row)
	if !CanMaterializeDirect(p) {
		t.Fatal(p)
	}
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) { return []byte{0x28, 1, 1, 2, 3}, nil }}
	out, e := MaterializeDirect(context.Background(), NewSession(c, nil), p)
	if e != nil || out.RuntimeLength != 3 || !out.Executable() {
		t.Fatalf("%+v %v", out, e)
	}
}
func TestRoutineMaskMaterialization(t *testing.T) {
	row := routineRow()
	row["fixed_request"] = false
	row["execution"] = "plan_only"
	row["start_static"] = "310111050001"
	row["output_mask_value"] = map[string]any{"bytes": "ff00"}
	row["output_mask_button"] = map[string]any{"bytes": "00f0"}
	p, e := MaterializeRoutine(ResolvePlan(row), []byte{0x12, 0xff}, []byte{0xff, 0x30})
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(p.Start, []byte{0x31, 1, 0x11, 5, 0x12, 0x31}) {
		t.Fatalf("%x", p.Start)
	}
}
func TestDirectPackingMasks(t *testing.T) {
	row := directRow()
	p, e := PackDirectRawValue(row, 2, 1)
	if e != nil || !bytes.Equal(p, []byte{0, 1}) {
		t.Fatalf("%x %v", p, e)
	}
	a, b, e := DirectControlEnableMasks(row, 2)
	if e != nil || !bytes.Equal(a, p) || !bytes.Equal(b, p) {
		t.Fatalf("%x %x %v", a, b, e)
	}
	if _, e := PackDirectRawValue(row, 2, 2); e == nil {
		t.Fatal("overflow accepted")
	}
}
func TestEngineeringConversion(t *testing.T) {
	r := directRow()
	r["signal_info"] = map[string]any{"physical": map[string]any{"mul": 2, "div": 1, "offset": 10, "decimal_point_count": 1}}
	v, e := DirectEngineeringToRaw(r, "3.0")
	if e != nil || v != 10 {
		t.Fatalf("%d %v", v, e)
	}
	if _, e := DirectEngineeringToRaw(r, "3.01"); e == nil {
		t.Fatal("precision lost")
	}
	if _, e := DirectEngineeringToRaw(r, "NaN"); e == nil {
		t.Fatal("NaN accepted")
	}
}

func TestV4MaterializedFrames(t *testing.T) {
	r := routineRow()
	for _, phase := range []string{"start", "stop", "result"} {
		r[phase+"_frames"] = []any{map[string]any{"materialized_static_request": r[phase+"_static"]}}
		delete(r, phase+"_static")
	}
	p := ResolvePlan(r)
	if !p.Executable() {
		t.Fatal(p.Refusals)
	}
	r["start_frames"] = []any{map[string]any{"materialized_static_request": "31011105"}, map[string]any{"materialized_static_request": "31011106"}}
	if ResolvePlan(r).Executable() {
		t.Fatal("ambiguous frames accepted")
	}
}
func TestV4DirectProbe(t *testing.T) {
	r := directRow()
	r["start_frames"] = []any{map[string]any{"materialized_prefix": r["start_prefix"]}}
	r["stop_frames"] = []any{map[string]any{"materialized_prefix": r["stop_prefix"]}}
	delete(r, "start_prefix")
	delete(r, "stop_prefix")
	delete(r, "runtime_length")
	r["execution"] = "plan_only"
	r["runtime_length_probe"] = map[string]any{"kind": "read_data_by_identifier_value_length", "selector": "0xCA", "response_prefix_length": 3, "frames": []any{map[string]any{"materialized_request": "222801", "positive_check": "62"}}}
	if !CanMaterializeDirect(ResolvePlan(r)) {
		t.Fatal(ResolvePlan(r))
	}
}
