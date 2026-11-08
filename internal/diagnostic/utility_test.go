package diagnostic

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestMalformedRoutineNeverPanics(t *testing.T) {
	r := routineRow()
	r["start_static"] = "31"
	r["fixed_request"] = false
	r["output_mask_value"] = map[string]any{"bytes": "ff"}
	r["output_mask_button"] = map[string]any{"bytes": ""}
	p := ResolvePlan(r)
	if CanMaterializeRoutine(p) {
		t.Fatal("malformed routine materializable")
	}
	if _, e := MaterializeRoutine(p, []byte{1}, nil); e == nil {
		t.Fatal("malformed routine accepted")
	}
}
func TestEmergencyStopFailureIsReturned(t *testing.T) {
	first := errors.New("poll failed")
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) {
		if q[1] == 3 {
			return nil, first
		}
		if q[1] == 2 {
			return nil, errors.New("stop failed")
		}
		return q[1:], nil
	}}
	_, e := RunTest(context.Background(), NewSession(c, nil), ResolvePlan(routineRow()), RunOptions{Execute: true, Hold: time.Second, PollInterval: time.Millisecond})
	if !errors.Is(e, first) || !strings.Contains(e.Error(), "stop failed") {
		t.Fatal(e)
	}
}
func TestSimpleUtilityExactGate(t *testing.T) {
	row := routineRow()
	row["kind"] = "simple_operation"
	row["execution"] = "plan_only"
	row["support_gate"] = map[string]any{"kind": "rid", "mode": "p6-standard", "identifier": 0x1105}
	p := ResolveUtilityPlan(row)
	if !p.Executable() {
		t.Fatal(p)
	}
	c := &scriptedClient{}
	_, e := RunUtility(context.Background(), NewSession(c, nil), p, RunOptions{Execute: true, Hold: time.Millisecond}, "p6-standard", func(context.Context, int) (bool, error) { return false, nil })
	if e == nil || len(c.calls) != 0 {
		t.Fatal(e, c.calls)
	}
}
