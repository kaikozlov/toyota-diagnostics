package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
	"golang.org/x/term"
)

type didQuery struct {
	id      int
	signals []map[string]any
}

func (r *runtimeApp) didQueries(ecu diagnostic.EcuSpec, queries []string, broad bool) ([]didQuery, error) {
	out := []didQuery{}
	seen := map[int]bool{}
	for _, query := range queries {
		id, rows, err := r.profile.ResolveDID(r.ctx, ecu, query)
		if err == nil {
			if !seen[id] {
				out = append(out, didQuery{id, rows})
				seen[id] = true
			}
			continue
		}
		if !broad {
			return nil, err
		}
		cat, e := r.profile.Category(r.ctx, ecu)
		if e != nil {
			return nil, e
		}
		dids := runtimeMap(cat["dids"])
		keys := make([]string, 0, len(dids))
		for key := range dids {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		matched := false
		for _, key := range keys {
			signals := runtimeRows(dids[key])
			for _, s := range signals {
				if strings.Contains(strings.ToLower(fmt.Sprint(s["name"])), strings.ToLower(query)) {
					n, e := runtimeInt(key)
					if e != nil {
						return nil, e
					}
					if !seen[n] {
						out = append(out, didQuery{n, signals})
						seen[n] = true
					}
					matched = true
					break
				}
			}
		}
		if !matched {
			return nil, err
		}
	}
	return out, nil
}
func didRecord(ecu diagnostic.EcuSpec, q didQuery, data []byte) map[string]any {
	signals := make([]map[string]any, 0, len(q.signals))
	for _, s := range q.signals {
		row := map[string]any{}
		for _, key := range []string{"name", "decoder", "bit_start", "bit_end", "unit"} {
			row[key] = s[key]
		}
		decoded, e := diagnostic.DecodeSignal(data, s)
		if e != nil {
			row["error"] = e.Error()
		} else {
			for k, v := range decoded {
				row[k] = v
			}
		}
		signals = append(signals, row)
	}
	return map[string]any{"ecu": map[string]any{"key": ecu.Key, "name": ecu.Name, "address": ecu.Address}, "did": q.id, "data_hex": hex.EncodeToString(data), "signals": signals}
}
func printDidRecord(row map[string]any) {
	ecu := runtimeMap(row["ecu"])
	fmt.Printf("%v DID 0x%04X: %v\n", ecu["name"], runtimeNumber(row["did"]), row["data_hex"])
	for _, s := range runtimeRows(row["signals"]) {
		if e := s["error"]; e != nil {
			fmt.Printf("  %v: decode unavailable: %v\n", s["name"], e)
			continue
		}
		v := s["value"]
		if s["pattern"] != nil {
			v = s["pattern"]
		}
		fmt.Printf("  %v: %v %v\n", s["name"], v, s["unit"])
	}
}
func (r *runtimeApp) didCommand(a []string) error {
	if len(a) < 3 {
		return usagef(a[0], "expected %s ACTION ECU [IDENTIFIER ...]", a[0])
	}
	action := a[1]
	if action == "watch" {
		return r.monitorCommand(append([]string{"monitor", a[2]}, a[3:]...))
	}
	if action != "decode" && action != "read" && action != "support" {
		return usagef(a[0], "unknown action %q", action)
	}
	if action != "decode" {
		if err := r.ensureVehicle(); err != nil {
			return err
		}
	}
	ecu, err := r.ecu(a[2])
	if err != nil {
		return err
	}
	if action == "support" {
		ids := []int{}
		for _, q := range a[3:] {
			id, e := runtimeInt(q)
			if a[0] == "did" {
				id, _, e = r.profile.ResolveDID(r.ctx, ecu, q)
			} else if e != nil {
				cat, ce := r.profile.Category(r.ctx, ecu)
				if ce != nil {
					return ce
				}
				rows := []map[string]any{}
				for _, row := range runtimeRows(cat["active_tests"]) {
					if row["kind"] == "routine" {
						rows = append(rows, row)
					}
				}
				row, ce := opLookup(rows, q, "id")
				if ce != nil {
					return ce
				}
				if row["routine_id"] == nil {
					return fmt.Errorf("routine %q has no known RID", q)
				}
				id = runtimeNumber(row["routine_id"])
				e = nil
			}
			if e != nil {
				return e
			}
			if id < 0 || id > 65535 {
				return fmt.Errorf("identifier out of range: %d", id)
			}
			ids = append(ids, id)
		}
		support, e := r.support(ecu, nil, a[0] == "rid")
		if e != nil {
			return e
		}
		if support.StaticIDs == nil {
			c, e := r.client(ecu)
			if e != nil {
				return e
			}
			support.Client = c
		}
		if len(a) == 3 {
			ids, e := support.SupportedIDs(r.ctx)
			if e != nil {
				return e
			}
			return r.output(map[string]any{"ecu": ecu.Key, "identifiers": ids})
		}
		items := []map[string]any{}
		for _, id := range ids {
			ok, e := support.Supports(r.ctx, id)
			if e != nil {
				return e
			}
			items = append(items, map[string]any{"id": id, "supported": ok})
		}
		return r.output(map[string]any{"ecu": ecu.Key, "items": items})
	}
	if action == "decode" {
		if len(a) != 5 {
			return usagef("did", "expected did decode ECU DID HEX")
		}
		q, e := r.didQueries(ecu, a[3:4], false)
		if e != nil {
			return e
		}
		data, e := runtimeHex(a[4])
		if e != nil {
			return e
		}
		record := didRecord(ecu, q[0], data)
		if r.opt.json {
			return r.output(record)
		}
		printDidRecord(record)
		return nil
	}
	if len(a) < 4 {
		return usagef("did", "did read needs at least one DID or name")
	}
	queries, e := r.didQueries(ecu, a[3:], false)
	if e != nil {
		return e
	}
	c, e := r.client(ecu)
	if e != nil {
		return e
	}
	values := []map[string]any{}
	for _, q := range queries {
		data, e := diagnostic.ReadDID(r.ctx, c, uint16(q.id))
		if e != nil {
			return e
		}
		record := didRecord(ecu, q, data)
		values = append(values, record)
		if !r.opt.json {
			printDidRecord(record)
		}
	}
	if r.opt.json {
		return r.output(map[string]any{"values": values})
	}
	return nil
}
func (r *runtimeApp) support(ecu diagnostic.EcuSpec, c diagnostic.RequestClient, routine bool) (*diagnostic.SupportResolver, error) {
	category, err := r.profile.SelectedCategory(r.ctx, ecu.CategoryID)
	if err != nil {
		return nil, err
	}
	family, _ := category["support_family"].(string)
	mode, _ := category["support_mode"].(string)
	contract := runtimeMap(runtimeMap(r.profile.Index["support_contracts"])[strings.ToLower(family)])
	static := routine && family == "p5" && ecu.Generation&31 == 0x15
	if static {
		mode = "p5-standard"
	}
	s, err := diagnostic.NewSupportResolver(c, contract, mode, routine)
	if err != nil {
		return nil, err
	}
	if routine && mode == "p5-standard" {
		cat, e := r.profile.Category(r.ctx, ecu)
		if e != nil {
			return nil, e
		}
		if static {
			ids, e := diagnostic.StaticRoutineIDs(runtimeRows(cat["active_tests"]), runtimeRows(cat["utilities"]))
			if e != nil {
				return nil, e
			}
			s.StaticIDs = append([]int{}, ids...)
		} else {
			if ecu.Generation&31 != 0x14 {
				return nil, fmt.Errorf("P5 RID generation-low5 0x%02X is not implemented", ecu.Generation&31)
			}
			for _, f := range runtimeRows(cat["functions"]) {
				if runtimeNumber(f["function_id"]) != 3 {
					continue
				}
				for _, id := range runtimeArray(f["detail_ids"]) {
					if runtimeNumber(id) == 0x56 {
						s.StripRoutineInfo = true
					}
				}
			}
		}
	}
	return s, nil
}

type samplingTarget struct {
	ecu      diagnostic.EcuSpec
	queries  []didQuery
	session  *diagnostic.Session
	nextKeep time.Time
}

func (t *samplingTarget) keepaliveIfDue(ctx context.Context, now time.Time) error {
	life := t.session.Lifecycle
	if life == nil || life.Keepalive == nil || now.Before(t.nextKeep) {
		return nil
	}
	if err := t.session.Keepalive(ctx); err != nil {
		return err
	}
	t.nextKeep = now.Add(life.Keepalive.Interval)
	return nil
}

func (r *runtimeApp) closeSamplingSessions() error {
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(r.ctx), 2*time.Second)
	defer cancel()
	var err error
	for i := len(r.sessions) - 1; i >= 0; i-- {
		if e := r.sessions[i].Close(cleanup); e != nil {
			err = errors.Join(err, fmt.Errorf("session cleanup: %w", e))
		}
	}
	return err
}

func (r *runtimeApp) startSamplingSession(ecu diagnostic.EcuSpec) (*diagnostic.Session, error) {
	s, err := r.session(ecu)
	if err != nil {
		return nil, err
	}
	if s.Lifecycle != nil {
		if err := s.EnterExtended(r.ctx); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (r *runtimeApp) monitorCommand(a []string) (err error) {
	if a[0] == "observe" {
		if len(a) < 2 {
			return usagef(a[0], "provide at least one ECU:DID_OR_TERM")
		}
	} else if len(a) < 3 {
		return usagef("monitor", "monitor needs an ECU and at least one DID/name")
	}
	interval, e := r.opt.duration("interval", 250*time.Millisecond)
	if e != nil {
		return e
	}
	count, e := runtimeInt(r.opt.value("count", "0"))
	if e != nil || count < 0 {
		if e != nil {
			return e
		}
		return fmt.Errorf("invalid integer %q", r.opt.value("count", "0"))
	}
	if err := r.ensureVehicle(); err != nil {
		return err
	}
	defer func() { err = errors.Join(err, r.closeSamplingSessions()) }()
	type target = samplingTarget
	var targets []target
	add := func(ref string, queries []string) error {
		ecu, e := r.ecu(ref)
		if e != nil {
			return e
		}
		q, e := r.didQueries(ecu, queries, true)
		if e != nil {
			return e
		}
		for i := range targets {
			if targets[i].ecu.Key == ecu.Key {
				targets[i].queries = append(targets[i].queries, q...)
				return nil
			}
		}
		targets = append(targets, target{ecu: ecu, queries: q})
		return nil
	}
	if a[0] == "observe" {
		for _, item := range a[1:] {
			if item == "tss3-longitudinal" {
				for _, v := range []string{"frc:0x1B03", "frc:0x1B04", "frc:0x1B05", "frc:0x1B06", "frc:0x1B07", "brake:0x10A1", "brake:0x10A2", "brake:0x10A3", "brake:0x10A4"} {
					ref, q, _ := strings.Cut(v, ":")
					if e := add(ref, []string{q}); e != nil {
						return e
					}
				}
				continue
			}
			ref, q, ok := strings.Cut(item, ":")
			if !ok {
				return fmt.Errorf("observe expects ECU:DID_OR_TERM, got %q", item)
			}
			if e := add(ref, []string{q}); e != nil {
				return e
			}
		}
	} else {
		if e := add(a[1], a[2:]); e != nil {
			return e
		}
	}
	for i := range targets {
		s, e := r.startSamplingSession(targets[i].ecu)
		if e != nil {
			return e
		}
		targets[i].session = s
		if s.Lifecycle != nil && s.Lifecycle.Keepalive != nil {
			targets[i].nextKeep = time.Now().Add(s.Lifecycle.Keepalive.Interval)
		}
	}
	read := func(ctx context.Context) ([]map[string]any, error) {
		now := time.Now()
		for i := range targets {
			if e := targets[i].keepaliveIfDue(ctx, now); e != nil {
				return nil, e
			}
		}
		values := []map[string]any{}
		for _, t := range targets {
			for _, q := range t.queries {
				data, e := diagnostic.ReadDID(ctx, t.session.Client, uint16(q.id))
				if e != nil {
					if ctx.Err() != nil {
						return nil, e
					}
					values = append(values, map[string]any{"ecu": map[string]any{"key": t.ecu.Key}, "did": q.id, "signals": []map[string]any{{"name": "(read)", "error": e.Error()}}})
					continue
				}
				values = append(values, didRecord(t.ecu, q, data))
			}
		}
		return values, nil
	}
	label := a[1]
	if a[0] == "observe" {
		label = "observe"
	}
	return diagnostic.RunMonitor(r.ctx, os.Stdout, label, read, diagnostic.MonitorOptions{Interval: interval, Count: count, Changed: r.opt.flag("changed"), JSONL: r.opt.json || r.opt.flag("jsonl"), CSV: r.opt.flag("csv"), Clear: term.IsTerminal(int(os.Stdout.Fd())) && !r.opt.flag("no-clear")})
}
func (r *runtimeApp) dtcCommand(a []string) error {
	if len(a) != 2 || (a[1] != "scan" && a[1] != "clear") {
		return usagef("dtc", "expected dtc scan or clear")
	}
	if err := r.ensureVehicle(); err != nil {
		return err
	}
	targets := r.profile.ECUs
	if refs := r.opt.values["ecu"]; len(refs) > 0 {
		targets = nil
		for _, ref := range refs {
			ecu, e := r.ecu(ref)
			if e != nil {
				return e
			}
			targets = append(targets, ecu)
		}
	}
	available := 0
	for _, ecu := range targets {
		if diagnosticRouteError(ecu) == nil {
			available++
		}
	}
	if available == 0 {
		return fmt.Errorf("no selected ECU has an implemented diagnostic route; no requests sent")
	}
	if err := r.connect(false); err != nil {
		return err
	}
	mask := runtimeNumber(r.profile.Index["fault_status_mask"])
	if mask == 0 {
		mask = 0xaf
	}
	scan := func() (map[string]any, []diagnostic.EcuSpec, error) {
		ecus := []map[string]any{}
		responders := []diagnostic.EcuSpec{}
		faults := 0
		for _, ecu := range targets {
			if e := diagnosticRouteError(ecu); e != nil {
				ecus = append(ecus, map[string]any{"key": ecu.Key, "error": e.Error()})
				continue
			}
			c, e := r.client(ecu)
			if e != nil {
				return nil, nil, e
			}
			data, e := c.Request(r.ctx, 0x19, []byte{2, 0xff})
			if e != nil {
				var negative *transport.NegativeResponseError
				if r.ctx.Err() != nil {
					return nil, nil, r.ctx.Err()
				}
				if !isTimeout(e) && !errors.As(e, &negative) {
					return nil, nil, e
				}
				ecus = append(ecus, map[string]any{"key": ecu.Key, "error": e.Error()})
				continue
			}
			if len(data) == 0 || data[0] != 2 {
				return nil, nil, fmt.Errorf("%s DTC response echo mismatch", ecu.Key)
			}
			records, e := diagnostic.ParseDTCResponse(data[1:])
			if e != nil {
				return nil, nil, e
			}
			responders = append(responders, ecu)
			items := []map[string]any{}
			cat, e := r.profile.Category(r.ctx, ecu)
			if e != nil {
				return nil, nil, e
			}
			for _, rec := range records {
				bits, _ := diagnostic.DecodeStatusBits(int(rec.Status))
				fault := int(rec.Status)&mask != 0
				if fault {
					faults++
				}
				descriptions := []map[string]any{}
				for _, rows := range runtimeMap(cat["dtcs"]) {
					for _, row := range runtimeRows(rows) {
						if strings.EqualFold(fmt.Sprint(row["code"]), rec.Code) {
							descriptions = append(descriptions, row)
						}
					}
				}
				if r.opt.flag("all") || r.opt.json || fault {
					items = append(items, map[string]any{"code": rec.Code, "status": rec.Status, "status_bits": bits, "fault_status": fault, "descriptions": descriptions})
				}
			}
			ecus = append(ecus, map[string]any{"key": ecu.Key, "name": ecu.Name, "address": ecu.Address, "category_id": ecu.CategoryID, "dtcs": items})
		}
		return map[string]any{"fault_status_mask": mask, "responding_ecus": len(responders), "fault_status_records": faults, "ecus": ecus}, responders, nil
	}
	before, responders, e := scan()
	if e != nil {
		return e
	}
	if len(responders) == 0 {
		if e := r.output(before); e != nil {
			return e
		}
		return fmt.Errorf("no ECU responded to the DTC scan; no clear requests sent")
	}
	if a[1] == "scan" {
		if e := r.output(before); e != nil {
			return e
		}
		if runtimeNumber(before["fault_status_records"]) > 0 {
			return exitStatus(1)
		}
		return nil
	}
	clears := []map[string]any{}
	for _, ecu := range responders {
		c, e := r.client(ecu)
		if e != nil {
			return e
		}
		c.Timeout = 2 * time.Second
		_, e = c.Request(r.ctx, 0x14, []byte{0xff, 0xff, 0xff})
		row := map[string]any{"ecu": ecu.Key, "cleared": e == nil}
		if e != nil {
			row["error"] = e.Error()
		}
		clears = append(clears, row)
	}
	// Legislated OBD clearing is a distinct broadcast, then every ECU is rescanned.
	expected := map[uint32]bool{}
	for _, ecu := range r.profile.ECUs {
		address := runtimeNumber(ecu.Route["legislated_request_address"])
		if address >= 0x7e0 && address <= 0x7e7 {
			expected[uint32(address+8)] = true
		}
	}
	positive, e := functionalOBD(r.ctx, r.transport, 4, nil, expected, r.profile.Bus, time.Second)
	if e != nil {
		return e
	}
	functionalComplete, functionalCompleteness := functionalClearCompleteness(expected, positive)
	timer := time.NewTimer(200 * time.Millisecond)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return r.ctx.Err()
	case <-timer.C:
	}
	after, _, e := scan()
	if e != nil {
		return e
	}
	verified := len(responders) > 0 && runtimeNumber(after["responding_ecus"]) == len(responders) && runtimeNumber(after["fault_status_records"]) == 0
	if e := r.output(map[string]any{"before": before, "clear": clears, "after": after, "functional_complete": functionalComplete, "functional_completeness": functionalCompleteness, "clear_verified": verified}); e != nil {
		return e
	}
	if !verified {
		return exitStatus(2)
	}
	return nil
}

func functionalClearCompleteness(expected, positive map[uint32]bool) (bool, string) {
	if len(expected) == 0 {
		return false, "unknown"
	}
	for addr := range expected {
		if !positive[addr] {
			return false, "incomplete"
		}
	}
	return true, "complete"
}
