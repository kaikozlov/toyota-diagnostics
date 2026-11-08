package diagnostic

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

type TestPlan struct {
	ID                             int
	Name, Kind, SessionRequirement string
	Identifier                     uint16
	Start, Stop, Status            []byte
	RuntimeLength                  int
	Parameterized                  bool
	Refusals                       []string
	Row                            map[string]any
	Members                        []TestPlan
	InputSlots                     []int
}

func (p TestPlan) Executable() bool { return len(p.Refusals) == 0 }
func (p TestPlan) Err() error {
	return fmt.Errorf("operation %04X %q is not executable: %s", p.ID, p.Name, strings.Join(p.Refusals, "; "))
}
func ResolvePlan(row map[string]any) TestPlan {
	row = normalizeOperationRow(row)
	p := TestPlan{ID: diagNum(row["id"]), Name: diagString(row["name"]), Kind: diagString(row["kind"]), SessionRequirement: diagString(row["session_requirement"]), Row: row}
	if _, e := diagInt(row["id"]); row["id"] != nil && e != nil {
		p.Refusals = append(p.Refusals, "malformed id")
	}
	sid := 0
	switch p.Kind {
	case "direct":
		sid = 0x2f
	case "routine":
		sid = 0x31
	default:
		p.Refusals = append(p.Refusals, "kind must be direct or routine")
		return p
	}
	if row["execution"] != "executable" {
		p.Refusals = append(p.Refusals, "runtime request geometry is not fully materialized")
	}
	if diagNum(row["service"]) != sid {
		p.Refusals = append(p.Refusals, "incorrect service")
	}
	if diagNum(row["positive_response"]) != sid+0x40 {
		p.Refusals = append(p.Refusals, "incorrect positive response SID")
	}
	switch p.SessionRequirement {
	case "none", "default", "extended":
	default:
		p.Refusals = append(p.Refusals, "missing or unsupported session requirement")
	}
	key := "did"
	startKey, stopKey := "start_prefix", "stop_prefix"
	if p.Kind == "routine" {
		key = "routine_id"
		startKey = "start_static"
		stopKey = "stop_static"
	}
	id, e := diagInt(row[key])
	if e != nil || id < 0 || id >= 65535 {
		p.Refusals = append(p.Refusals, "invalid or unresolved identifier")
	} else {
		p.Identifier = uint16(id)
	}
	p.Start, e = diagBytes(row[startKey])
	if e != nil {
		p.Refusals = append(p.Refusals, "malformed start bytes")
	}
	p.Stop, e = diagBytes(row[stopKey])
	if e != nil {
		p.Refusals = append(p.Refusals, "malformed stop bytes")
	}
	validate := func(q []byte, control byte) bool {
		if len(q) < 4 || q[0] != byte(sid) {
			return false
		}
		if p.Kind == "direct" {
			return q[1] == byte(id>>8) && q[2] == byte(id) && q[3] <= 3
		}
		return q[1] == control && q[2] == byte(id>>8) && q[3] == byte(id)
	}
	if !validate(p.Start, 1) {
		p.Refusals = append(p.Refusals, "start request geometry mismatch")
	}
	if !validate(p.Stop, 2) {
		p.Refusals = append(p.Refusals, "stop request geometry mismatch")
	}
	if p.Kind == "direct" {
		if row["multi_control_group"] != nil && row["multi_control_group"] != false {
			p.Refusals = append(p.Refusals, "use grouped composer for multi-control group")
		}
		p.RuntimeLength, e = diagInt(row["runtime_length"])
		if e != nil || p.RuntimeLength <= 0 {
			p.RuntimeLength = 0
			p.Refusals = append(p.Refusals, "runtime payload length not definitively recovered")
		}
	} else {
		p.Parameterized = row["fixed_request"] != true
		if row["result_static"] != nil {
			p.Status, e = diagBytes(row["result_static"])
			if e != nil || !validate(p.Status, 3) {
				p.Refusals = append(p.Refusals, "status request geometry mismatch")
			}
		}
	}
	return p
}
func RuntimeRefusals(s *Session, p TestPlan) []string {
	r := append([]string(nil), p.Refusals...)
	if p.SessionRequirement == "extended" && (s == nil || s.Lifecycle == nil) {
		r = append(r, "registry supplies no recovered session lifecycle")
	}
	return r
}
func directProbe(p TestPlan) bool {
	row := p.Row
	if p.Kind != "direct" || p.RuntimeLength != 0 || (row["execution"] != "plan_only" && row["execution"] != "executable") || row["multi_control_group"] == true {
		return false
	}
	q := []byte{0x22, byte(p.Identifier >> 8), byte(p.Identifier)}
	if m := diagMap(row["runtime_length_probe"]); m != nil {
		req, e := diagBytes(m["request"])
		check, ce := diagBytes(m["check"])
		return e == nil && ce == nil && m["kind"] == "read_data_by_identifier_value_length" && diagNum(m["selector"]) == 0xca && bytes.Equal(req, q) && bytes.Equal(check, []byte{0x62}) && diagNum(m["response_prefix_length"]) == 3
	}
	m := diagMap(row["initial_read"])
	if m == nil {
		return false
	}
	req, e := diagBytes(m["request"])
	check, ce := diagBytes(m["check"])
	return e == nil && ce == nil && diagNum(m["mode"]) == 0 && bytes.Equal(req, q) && bytes.Equal(check, []byte{0x62})
}
func CanMaterializeDirect(p TestPlan) bool { return directProbe(p) }
func MaterializeDirect(ctx context.Context, s *Session, p TestPlan) (TestPlan, error) {
	if !directProbe(p) {
		return p, p.Err()
	}
	minimum := 1
	if p.Row["runtime_length_minimum"] != nil {
		var e error
		minimum, e = diagInt(p.Row["runtime_length_minimum"])
		if e != nil || minimum <= 0 {
			return p, fmt.Errorf("invalid runtime length minimum")
		}
	}
	row := diagClone(p.Row)
	row["execution"] = "executable"
	row["runtime_length"] = minimum
	shape := ResolvePlan(row)
	if r := RuntimeRefusals(s, shape); len(r) > 0 {
		shape.Refusals = r
		return p, shape.Err()
	}
	if p.SessionRequirement == "extended" {
		if e := s.EnterExtended(ctx); e != nil {
			return p, e
		}
	}
	data, e := ReadDID(ctx, s.Client, p.Identifier)
	if e != nil {
		return p, e
	}
	if len(data) < minimum {
		return p, fmt.Errorf("runtime length probe returned %d bytes below minimum %d", len(data), minimum)
	}
	row["runtime_length"] = len(data)
	return ResolvePlan(row), nil
}
func routineMasks(p TestPlan) ([]byte, []byte, int, error) {
	if p.Kind != "routine" || !p.Parameterized || len(p.Start) < 4 {
		return nil, nil, 0, fmt.Errorf("routine is not parameterized")
	}
	if p.Row["execution"] != "plan_only" && p.Row["execution"] != "executable" {
		return nil, nil, 0, p.Err()
	}
	parse := func(key string) ([]byte, error) {
		m := diagMap(p.Row[key])
		if m == nil {
			return nil, fmt.Errorf("missing %s", key)
		}
		v := m["bytes"]
		if v == nil || v == "" {
			return nil, nil
		}
		return diagBytes(v)
	}
	v, e := parse("output_mask_value")
	if e != nil {
		return nil, nil, 0, e
	}
	b, e := parse("output_mask_button")
	if e != nil {
		return nil, nil, 0, e
	}
	n := 0
	if len(p.Start) >= 4 {
		n = len(p.Start) - 4
	}
	if n == 0 {
		n = len(v)
	}
	if n == 0 {
		n = len(b)
	}
	if n == 0 || len(v)+len(b) == 0 || (len(v) != 0 && len(v) != n) || (len(b) != 0 && len(b) != n) {
		return nil, nil, 0, fmt.Errorf("inconsistent routine mask widths")
	}
	return v, b, n, nil
}
func CanMaterializeRoutine(p TestPlan) bool { _, _, _, e := routineMasks(p); return e == nil }
func MaterializeRoutine(p TestPlan, value, button []byte) (TestPlan, error) {
	vm, bm, n, e := routineMasks(p)
	if e != nil {
		return p, e
	}
	if (len(vm) > 0 && len(value) != n) || (len(vm) == 0 && value != nil) {
		return p, fmt.Errorf("value must match recovered routine mask width %d", n)
	}
	if (len(bm) > 0 && len(button) != n) || (len(bm) == 0 && button != nil) {
		return p, fmt.Errorf("button must match recovered routine mask width %d", n)
	}
	merged := make([]byte, n)
	copy(merged, p.Start[4:])
	for i := range merged {
		if len(vm) > 0 {
			merged[i] |= value[i] & vm[i]
		}
		if len(bm) > 0 {
			merged[i] |= button[i] & bm[i]
		}
	}
	row := diagClone(p.Row)
	row["execution"] = "executable"
	row["fixed_request"] = true
	row["start_static"] = hex.EncodeToString(append(bytes.Clone(p.Start[:4]), merged...))
	out := ResolvePlan(row)
	if !out.Executable() {
		return out, out.Err()
	}
	return out, nil
}

type RunOptions struct {
	Execute                                  bool
	Hold, PollInterval                       time.Duration
	Value, OptionRecord, StartMask, StopMask []byte
}
type StatusSample struct {
	At   time.Time
	Data []byte
}
type ActiveTestResult struct {
	Plan          TestPlan
	Executed      bool
	Start, Stop   []byte
	Statuses      []StatusSample
	CleanupErrors []string
}

func operationRequest(ctx context.Context, c RequestClient, q []byte) ([]byte, error) {
	if len(q) < 4 {
		return nil, fmt.Errorf("incomplete operation request")
	}
	r, e := c.Request(ctx, q[0], q[1:])
	if e != nil {
		return nil, e
	}
	if len(r) < 3 || !bytes.Equal(r[:3], q[1:4]) {
		return nil, fmt.Errorf("operation response echo mismatch: %x", r)
	}
	return r[3:], nil
}
func prepareOperation(ctx context.Context, s *Session, p TestPlan, execute bool) error {
	if !execute {
		return nil
	}
	if r := RuntimeRefusals(s, p); len(r) > 0 {
		p.Refusals = r
		return p.Err()
	}
	if p.SessionRequirement == "extended" {
		return s.EnterExtended(ctx)
	}
	return nil
}
func RunTest(ctx context.Context, s *Session, p TestPlan, o RunOptions) (result ActiveTestResult, err error) {
	result.Plan = p
	if o.Hold <= 0 {
		return result, fmt.Errorf("hold must be positive")
	}
	if !o.Execute {
		return result, nil
	}
	if p.Kind == "direct" && len(o.Value) != p.RuntimeLength {
		return result, fmt.Errorf("direct value must be exactly %d bytes", p.RuntimeLength)
	}
	if p.Kind == "routine" && ((p.Parameterized && len(o.OptionRecord) == 0) || (!p.Parameterized && len(o.OptionRecord) > 0)) {
		return result, fmt.Errorf("routine option record disagrees with fixed/parameterized geometry")
	}
	if e := prepareOperation(ctx, s, p, true); e != nil {
		return result, e
	}
	start, stop := bytes.Clone(p.Start), bytes.Clone(p.Stop)
	if p.Kind == "direct" {
		start = append(append(start, o.Value...), o.StartMask...)
		stop = append(stop, o.StopMask...)
	} else if p.Kind == "routine" {
		start = append(start, o.OptionRecord...)
	} else {
		return result, fmt.Errorf("unsupported operation kind %s", p.Kind)
	}
	result.Start, err = operationRequest(ctx, s.Client, start)
	if err != nil {
		return result, err
	}
	result.Executed = true
	stopped := false
	defer func() {
		if !stopped {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			_, ce := operationRequest(cleanup, s.Client, stop)
			if ce != nil {
				result.CleanupErrors = append(result.CleanupErrors, "emergency stop: "+ce.Error())
				err = errors.Join(err, fmt.Errorf("emergency stop: %w", ce))
			}
		}
		result.CleanupErrors = append(result.CleanupErrors, s.CleanupErrors...)
	}()
	deadline := time.NewTimer(o.Hold)
	defer deadline.Stop()
	var poll, keep *time.Ticker
	var pollC, keepC <-chan time.Time
	if len(p.Status) > 0 {
		interval := o.PollInterval
		if interval <= 0 {
			interval = 500 * time.Millisecond
		}
		poll = time.NewTicker(interval)
		defer poll.Stop()
		pollC = poll.C
	}
	if s.Lifecycle != nil && s.Lifecycle.Keepalive != nil {
		keep = time.NewTicker(s.Lifecycle.Keepalive.Interval)
		defer keep.Stop()
		keepC = keep.C
	}
loop:
	for {
		select {
		case <-ctx.Done():
			return result, ctx.Err()
		case <-deadline.C:
			break loop
		case at := <-pollC:
			r, e := operationRequest(ctx, s.Client, p.Status)
			if e != nil {
				return result, e
			}
			result.Statuses = append(result.Statuses, StatusSample{At: at, Data: r})
		case <-keepC:
			if e := s.Keepalive(ctx); e != nil {
				return result, e
			}
		}
	}
	result.Stop, err = operationRequest(ctx, s.Client, stop)
	if err == nil {
		stopped = true
	}
	return result, err
}
func StopTest(ctx context.Context, s *Session, p TestPlan, mask []byte, execute bool) (ActiveTestResult, error) {
	r := ActiveTestResult{Plan: p}
	if e := prepareOperation(ctx, s, p, execute); e != nil {
		return r, e
	}
	if !execute {
		return r, nil
	}
	q := bytes.Clone(p.Stop)
	if p.Kind == "direct" {
		q = append(q, mask...)
	}
	var e error
	r.Stop, e = operationRequest(ctx, s.Client, q)
	r.Executed = e == nil
	return r, e
}

// Runtime v4 retains every frame alternative rather than flattening to one.
// Only unanimous, fully materialized wire bytes are executable.
func uniqueOperationFrame(v any, key string) any {
	rows, ok := v.([]any)
	if !ok || len(rows) == 0 {
		return nil
	}
	var selected []byte
	for _, v := range rows {
		m := diagMap(v)
		b, e := diagBytes(m[key])
		if e != nil || len(b) == 0 {
			return nil
		}
		if selected != nil && !bytes.Equal(selected, b) {
			return nil
		}
		selected = b
	}
	return hex.EncodeToString(selected)
}
func normalizeOperationRow(original map[string]any) map[string]any {
	row := diagClone(original)
	fields := map[string][2]string{"start_static": {"start_frames", "materialized_static_request"}, "stop_static": {"stop_frames", "materialized_static_request"}, "result_static": {"result_frames", "materialized_static_request"}}
	if row["kind"] == "direct" {
		fields = map[string][2]string{"start_prefix": {"start_frames", "materialized_prefix"}, "stop_prefix": {"stop_frames", "materialized_prefix"}}
	}
	for dest, source := range fields {
		if row[dest] == nil {
			row[dest] = uniqueOperationFrame(row[source[0]], source[1])
			if dest == "result_static" && row[dest] == nil && row[source[0]] != nil {
				if a, ok := row[source[0]].([]any); ok && len(a) > 0 {
					row[dest] = "invalid-unresolved-result-frames"
				}
			}
		}
	}
	if probe := diagMap(row["runtime_length_probe"]); probe != nil && probe["frames"] != nil {
		m := diagClone(probe)
		if m["request"] == nil {
			m["request"] = uniqueOperationFrame(m["frames"], "materialized_request")
		}
		if m["check"] == nil {
			m["check"] = uniqueOperationFrame(m["frames"], "positive_check")
		}
		row["runtime_length_probe"] = m
	}
	return row
}

func (p TestPlan) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"id": p.ID, "name": p.Name, "kind": p.Kind, "session_requirement": p.SessionRequirement, "identifier": p.Identifier, "start_hex": hex.EncodeToString(p.Start), "stop_hex": hex.EncodeToString(p.Stop), "status_hex": hex.EncodeToString(p.Status), "runtime_length": p.RuntimeLength, "parameterized": p.Parameterized, "runtime_executable": p.Executable(), "runtime_refusals": p.Refusals, "wire_plan": p.Row, "members": p.Members, "input_slots": p.InputSlots})
}
func (r ActiveTestResult) MarshalJSON() ([]byte, error) {
	statuses := make([]map[string]any, 0, len(r.Statuses))
	for _, s := range r.Statuses {
		statuses = append(statuses, map[string]any{"at": s.At, "payload_hex": hex.EncodeToString(s.Data)})
	}
	return json.Marshal(map[string]any{"plan": r.Plan, "executed": r.Executed, "start_hex": hex.EncodeToString(r.Start), "stop_hex": hex.EncodeToString(r.Stop), "statuses": statuses, "cleanup_errors": r.CleanupErrors})
}
