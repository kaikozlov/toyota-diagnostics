package diagnostic

import (
	"context"
	"fmt"
)

func ResolveUtilityPlan(row map[string]any) TestPlan {
	if row["kind"] != "simple_operation" {
		return ResolvePlan(row)
	}
	m := diagClone(row)
	m["kind"] = "routine"
	m["service"] = 0x31
	m["fixed_request"] = true
	gate := diagMap(row["support_gate"])
	id, e := diagInt(gate["identifier"])
	rid, re := diagInt(row["routine_id"])
	if row["execution"] == "plan_only" && gate["kind"] == "rid" && (gate["mode"] == "p5-standard" || gate["mode"] == "p6-standard") && e == nil && re == nil && id == rid {
		m["execution"] = "executable"
	}
	return ResolvePlan(m)
}
func RunUtility(ctx context.Context, s *Session, p TestPlan, o RunOptions, supportMode string, support SupportCheck) (ActiveTestResult, error) {
	if o.Execute && p.Row["support_gate"] != nil {
		if r := RuntimeRefusals(s, p); len(r) > 0 {
			p.Refusals = r
			return ActiveTestResult{Plan: p}, p.Err()
		}
		gate := diagMap(p.Row["support_gate"])
		id, e := diagInt(gate["identifier"])
		if gate == nil || gate["kind"] != "rid" || gate["mode"] != supportMode || e != nil || p.Kind != "routine" || id != int(p.Identifier) {
			return ActiveTestResult{Plan: p}, fmt.Errorf("utility support gate disagrees with selected Toyota route")
		}
		if support == nil {
			return ActiveTestResult{Plan: p}, fmt.Errorf("utility requires Toyota RID inventory")
		}
		if p.SessionRequirement == "extended" {
			if e := s.EnterExtended(ctx); e != nil {
				return ActiveTestResult{Plan: p}, e
			}
		}
		ok, e := support(ctx, id)
		if e != nil {
			return ActiveTestResult{Plan: p}, e
		}
		if !ok {
			return ActiveTestResult{Plan: p}, fmt.Errorf("Toyota inventory does not advertise utility RID %04X", id)
		}
	}
	return RunTest(ctx, s, p, o)
}
func MaterializeMultiDirect(ctx context.Context, s *Session, p TestPlan) (TestPlan, error) {
	if p.Kind != "multi_direct" || p.RuntimeLength != 0 || len(p.Members) == 0 || len(p.Refusals) != 1 || p.Refusals[0] != "runtime payload length not definitively known" {
		return p, p.Err()
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
	if len(data) == 0 {
		return p, fmt.Errorf("multi-control probe returned no data")
	}
	for _, m := range p.Members {
		minimum := 1
		if m.Row["runtime_length_minimum"] != nil {
			minimum, e = diagInt(m.Row["runtime_length_minimum"])
			if e != nil || minimum <= 0 {
				return p, fmt.Errorf("invalid member minimum")
			}
		}
		if len(data) < minimum {
			return p, fmt.Errorf("multi-control probe below member %04X minimum", m.ID)
		}
	}
	p.RuntimeLength = len(data)
	p.Refusals = nil
	return p, nil
}
func RunMultiDirect(ctx context.Context, s *Session, p TestPlan, o RunOptions, values map[int]uint64) (ActiveTestResult, error) {
	if !o.Execute {
		return ActiveTestResult{Plan: p}, nil
	}
	payload, start, stop, e := ComposeMultiDirectPayload(p, values)
	if e != nil {
		return ActiveTestResult{Plan: p}, e
	}
	direct := p
	direct.Kind = "direct"
	direct.Start = []byte{0x2f, byte(p.Identifier >> 8), byte(p.Identifier), 3}
	direct.Stop = []byte{0x2f, byte(p.Identifier >> 8), byte(p.Identifier), 0}
	o.Value = payload
	o.StartMask = start
	o.StopMask = stop
	r, e := RunTest(ctx, s, direct, o)
	r.Plan = p
	return r, e
}
