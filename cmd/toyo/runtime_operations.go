package main

import (
	"encoding/hex"
	"fmt"
	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"strconv"
	"strings"
	"time"
)

func opMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func opRows(v any) []map[string]any {
	var out []map[string]any
	switch rows := v.(type) {
	case []any:
		for _, v := range rows {
			if m := opMap(v); m != nil {
				out = append(out, m)
			}
		}
	case []map[string]any:
		out = rows
	}
	return out
}
func opText(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func opInt(v any) int { n, _ := strconv.ParseInt(opText(v), 0, 64); return int(n) }
func opMatch(rows []map[string]any, query, key string) []map[string]any {
	var exact, partial []map[string]any
	n, e := strconv.ParseInt(query, 0, 64)
	for _, m := range rows {
		if e == nil {
			if opInt(m[key]) == int(n) {
				exact = append(exact, m)
			}
			continue
		}
		name := opText(m["name"])
		if strings.EqualFold(name, query) {
			exact = append(exact, m)
		} else if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
			partial = append(partial, m)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return partial
}
func opLookup(rows []map[string]any, query, key string) (map[string]any, error) {
	m := opMatch(rows, query, key)
	if len(m) == 1 {
		return m[0], nil
	}
	return nil, fmt.Errorf("%q resolves %d matches; use an exact item ID", query, len(m))
}
func (r *runtimeApp) opSupport(ecu diagnostic.EcuSpec, s *diagnostic.Session, routine bool) (*diagnostic.SupportResolver, string, error) {
	meta := opMap(opMap(r.profile.Regional["selected_categories"])[strconv.Itoa(ecu.CategoryID)])
	mode := opText(meta["support_mode"])
	resolver, e := r.support(ecu, s.Client, routine)
	if e != nil {
		return nil, mode, e
	}
	return resolver, mode, nil
}
func (r *runtimeApp) operations(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: toyo %s <list|plan|run|stop>", args[0])
	}
	if args[0] == "customize" {
		return r.customizeOperations(args[1:])
	}
	action := args[1]
	switch action {
	case "list", "plan", "run", "stop", "groups", "group-plan", "group-run", "group-stop":
	default:
		return fmt.Errorf("unknown operation %q", action)
	}
	utility := args[0] == "utility"
	groups := action == "groups" || strings.HasPrefix(action, "group-")
	if utility && ((action == "list" && len(args) == 2) || (action == "plan" && len(args) == 3)) {
		rows := opRows(opMap(r.profile.Regional["utilities"])["bindings"])
		if action == "plan" {
			var matches []map[string]any
			for _, row := range rows {
				if strings.Contains(strings.ToLower(fmt.Sprint(row)), strings.ToLower(args[2])) {
					matches = append(matches, row)
				}
			}
			return r.operationRows(matches)
		}
		return r.operationRows(rows)
	}
	if action == "list" || action == "groups" {
		return r.operationList(args)
	}
	if len(args) < 4 {
		return fmt.Errorf("usage: toyo %s %s ECU ITEM", args[0], action)
	}
	ecu, e := r.ecu(args[2])
	if e != nil {
		return e
	}
	category, e := r.profile.Category(r.ctx, ecu)
	if e != nil {
		return e
	}
	key := "active_tests"
	if utility {
		key = "utilities"
	}
	rows := opRows(category[key])
	idKey := "id"
	if groups {
		rows = opRows(opMap(category["active_test_groups"])["groups"])
		idKey = "group_id"
	}
	kind := r.opt.value("kind", "")
	if kind != "" {
		filtered := []map[string]any{}
		for _, row := range rows {
			if row["kind"] == kind {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	row, e := opLookup(rows, args[3], idKey)
	if e != nil {
		return e
	}
	p := diagnostic.ResolvePlan(row)
	if utility {
		p = diagnostic.ResolveUtilityPlan(row)
	}
	if groups {
		p = diagnostic.ResolveMultiDirectGroup(row, func(id int) (map[string]any, error) {
			return opLookup(opRows(category["active_tests"]), strconv.Itoa(id), "id")
		})
	}
	if action == "plan" || action == "group-plan" || !r.opt.flag("execute") {
		return r.operationPlan(ecu, p, groups)
	}
	if action != "run" && action != "stop" && action != "group-run" && action != "group-stop" {
		return fmt.Errorf("unknown operation %q", action)
	}
	hold, e := r.opt.duration("hold", time.Second)
	if e != nil {
		return e
	}
	poll, e := r.opt.duration("poll-interval", 500*time.Millisecond)
	if e != nil {
		return e
	}
	if hold <= 0 || poll <= 0 {
		return fmt.Errorf("--hold and --poll-interval must be positive")
	}
	if !groups && !p.Executable() && !diagnostic.CanMaterializeDirect(p) && !diagnostic.CanMaterializeRoutine(p) {
		return p.Err()
	}
	if !utility && !groups {
		if p.Kind == "routine" {
			for _, key := range []string{"raw-value", "engineering-value", "choice"} {
				if r.opt.value(key, "") != "" {
					return fmt.Errorf("routine controls use --value/--button, not --%s", key)
				}
			}
			if !diagnostic.CanMaterializeRoutine(p) && (r.opt.value("value", "") != "" || r.opt.value("button", "") != "") {
				return fmt.Errorf("fixed routine does not accept --value or --button")
			}
		} else if p.Kind == "direct" && (r.opt.value("value", "") != "" || r.opt.value("button", "") != "") {
			return fmt.Errorf("direct controls use --raw-value, --engineering-value or --choice")
		}
	}
	bytesOption := func(key string) ([]byte, error) {
		text := r.opt.value(key, "")
		if text == "" {
			return nil, nil
		}
		return hex.DecodeString(strings.ReplaceAll(strings.TrimPrefix(text, "0x"), " ", ""))
	}
	if diagnostic.CanMaterializeRoutine(p) {
		value, e := bytesOption("value")
		if e != nil {
			return e
		}
		button, e := bytesOption("button")
		if e != nil {
			return e
		}
		p, e = diagnostic.MaterializeRoutine(p, value, button)
		if e != nil {
			return e
		}
	}
	var directValue uint64
	if !groups && !utility && p.Kind == "direct" && action == "run" {
		count := 0
		var value uint64
		for _, key := range []string{"raw-value", "engineering-value", "choice"} {
			text := r.opt.value(key, "")
			if text == "" {
				continue
			}
			count++
			switch key {
			case "raw-value":
				value, e = strconv.ParseUint(text, 0, 64)
			case "engineering-value":
				value, e = diagnostic.DirectEngineeringToRaw(row, text)
			case "choice":
				value, e = diagnostic.DirectChoiceToRaw(row, text)
			}
			if e != nil {
				return e
			}
		}
		if count != 1 {
			return fmt.Errorf("direct control requires exactly one of --raw-value, --engineering-value, --choice")
		}
		minimum := p.RuntimeLength
		if minimum <= 0 {
			minimum = opInt(row["runtime_length_minimum"])
		}
		if minimum <= 0 {
			minimum = 1
		}
		if _, e := diagnostic.PackDirectRawValue(row, minimum, value); e != nil {
			return e
		}
		directValue = value
		if _, _, e := diagnostic.DirectControlEnableMasks(row, minimum); e != nil {
			return e
		}
	}
	if groups && (len(p.Refusals) != 1 || p.Refusals[0] != "runtime payload length not definitively recovered") {
		return p.Err()
	}
	values := map[int]uint64{}
	if groups {
		for _, key := range []string{"member-raw-value", "member-engineering-value", "member-choice"} {
			for _, entry := range r.opt.values[key] {
				parts := strings.SplitN(entry, "=", 2)
				if len(parts) != 2 {
					return fmt.Errorf("%s requires MEMBER_ID=value", key)
				}
				id, err := strconv.ParseInt(parts[0], 0, 64)
				if err != nil {
					return err
				}
				if _, ok := values[int(id)]; ok {
					return fmt.Errorf("duplicate member value %04X", id)
				}
				var member *diagnostic.TestPlan
				for i := range p.Members {
					if p.Members[i].ID == int(id) {
						member = &p.Members[i]
					}
				}
				if member == nil {
					return fmt.Errorf("unknown member %04X", id)
				}
				var v uint64
				switch key {
				case "member-raw-value":
					v, err = strconv.ParseUint(parts[1], 0, 64)
				case "member-engineering-value":
					v, err = diagnostic.DirectEngineeringToRaw(member.Row, parts[1])
				case "member-choice":
					v, err = diagnostic.DirectChoiceToRaw(member.Row, parts[1])
				}
				if err != nil {
					return err
				}
				values[int(id)] = v
			}
		}

		for _, member := range p.Members {
			v, ok := values[member.ID]
			if action == "group-stop" {
				v = 0
				values[member.ID] = 0
				ok = true
			}
			if !ok {
				return fmt.Errorf("missing value for member %04X", member.ID)
			}
			minimum := opInt(member.Row["runtime_length_minimum"])
			if minimum <= 0 {
				minimum = 1
			}
			if _, e := diagnostic.PackDirectRawValue(member.Row, minimum, v); e != nil {
				return e
			}
			if _, _, e := diagnostic.DirectControlEnableMasks(member.Row, minimum); e != nil {
				return e
			}
		}
	}
	options := diagnostic.RunOptions{Execute: true, Hold: hold, PollInterval: poll}
	if utility {
		// Parse every supplied byte option before opening any transport, even when
		// its presence will subsequently be rejected for this operation kind.
		value, err := bytesOption("value")
		if err != nil {
			return fmt.Errorf("--value: %w", err)
		}
		mask, err := bytesOption("mask")
		if err != nil {
			return fmt.Errorf("--mask: %w", err)
		}
		option, err := bytesOption("option-record")
		if err != nil {
			return fmt.Errorf("--option-record: %w", err)
		}
		if p.Kind == "direct" {
			if len(option) > 0 {
				return fmt.Errorf("direct utility does not accept --option-record")
			}
			if p.RuntimeLength > 0 && len(value) != p.RuntimeLength {
				return fmt.Errorf("utility value must be exactly %d bytes", p.RuntimeLength)
			}
			if p.RuntimeLength == 0 {
				minimum := opInt(p.Row["runtime_length_minimum"])
				if minimum <= 0 {
					minimum = 1
				}
				if len(value) < minimum {
					return fmt.Errorf("utility value must supply at least the recovered minimum %d bytes", minimum)
				}
			}
			options.Value = value
			options.StopMask = mask
		} else {
			if len(value) > 0 || len(mask) > 0 {
				return fmt.Errorf("routine utility uses --option-record, not --value or --mask")
			}
			if p.Parameterized && len(option) == 0 {
				return fmt.Errorf("parameterized utility requires --option-record")
			}
			if !p.Parameterized && len(option) > 0 {
				return fmt.Errorf("fixed utility does not accept --option-record")
			}
			options.OptionRecord = option
		}
	}
	if e := r.ensureVehicle(); e != nil {
		return e
	}
	ecu, e = r.ecu(args[2])
	if e != nil {
		return e
	}
	s, e := r.session(ecu, row)
	if e != nil {
		return e
	}
	// P6 direct controls require the advertised support gate before the length probe.
	if gate := opMap(row["support_gate"]); gate != nil && !utility {
		if p.SessionRequirement == "extended" {
			if e := s.EnterExtended(r.ctx); e != nil {
				return e
			}
		}
		resolver, mode, e := r.opSupport(ecu, s, p.Kind == "routine")
		if e != nil {
			return e
		}
		id := opInt(gate["identifier"])
		wantKind := "did"
		if p.Kind == "routine" {
			wantKind = "rid"
		}
		if mode != opText(gate["mode"]) || id != int(p.Identifier) || gate["kind"] != wantKind {
			return fmt.Errorf("active-test support gate disagrees with Toyota route")
		}
		ok, e := resolver.Supports(r.ctx, id)
		if e != nil {
			return e
		}
		if !ok {
			return fmt.Errorf("Toyota inventory does not advertise identifier %04X", id)
		}
	}
	if diagnostic.CanMaterializeDirect(p) {
		p, e = diagnostic.MaterializeDirect(r.ctx, s, p)
		if e != nil {
			return e
		}
	}
	if groups {
		p, e = diagnostic.MaterializeMultiDirect(r.ctx, s, p)
		if e != nil {
			return e
		}
		if action == "group-stop" {
			for _, m := range p.Members {
				values[m.ID] = 0
			}
			_, _, mask, e := diagnostic.ComposeMultiDirectPayload(p, values)
			if e != nil {
				return e
			}
			direct := p
			direct.Kind = "direct"
			direct.Stop = []byte{0x2f, byte(p.Identifier >> 8), byte(p.Identifier), 0}
			result, e := diagnostic.StopTest(r.ctx, s, direct, mask, true)
			if e != nil {
				return e
			}
			if e := s.Close(r.ctx); e != nil {
				return fmt.Errorf("session cleanup: %w", e)
			}
			return r.operationResult(result)
		}
		result, e := diagnostic.RunMultiDirect(r.ctx, s, p, options, values)
		if e != nil {
			return e
		}
		if e := s.Close(r.ctx); e != nil {
			return fmt.Errorf("session cleanup: %w", e)
		}
		return r.operationResult(result)
	}
	if p.Kind == "direct" && !utility {
		options.StartMask, options.StopMask, e = diagnostic.DirectControlEnableMasks(p.Row, p.RuntimeLength)
		if e != nil {
			return e
		}
		if action != "stop" {
			options.Value, e = diagnostic.PackDirectRawValue(row, p.RuntimeLength, directValue)
			if e != nil {
				return e
			}
		}
	}
	if action == "stop" {
		result, e := diagnostic.StopTest(r.ctx, s, p, options.StopMask, true)
		if e != nil {
			return e
		}
		if e := s.Close(r.ctx); e != nil {
			return fmt.Errorf("session cleanup: %w", e)
		}
		return r.operationResult(result)
	}
	if utility {
		var check diagnostic.SupportCheck
		mode := ""
		if p.Row["support_gate"] != nil {
			resolver, m, e := r.opSupport(ecu, s, true)
			if e != nil {
				return e
			}
			check = resolver.Supports
			mode = m
		}
		result, e := diagnostic.RunUtility(r.ctx, s, p, options, mode, check)
		if e != nil {
			return e
		}
		if e := s.Close(r.ctx); e != nil {
			return fmt.Errorf("session cleanup: %w", e)
		}
		return r.operationResult(result)
	}
	result, e := diagnostic.RunTest(r.ctx, s, p, options)
	if e != nil {
		return e
	}
	if e := s.Close(r.ctx); e != nil {
		return fmt.Errorf("session cleanup: %w", e)
	}
	return r.operationResult(result)
}
func (r *runtimeApp) customizeOperations(args []string) error {
	member := opText(r.profile.Regional["customize_member"])
	if member == "" {
		return fmt.Errorf("region has no Customize catalog")
	}
	catalog, e := r.db.Document(r.ctx, member)
	if e != nil {
		return e
	}
	if len(args) == 0 {
		return fmt.Errorf("Customize requires a command")
	}
	action := args[0]
	if action == "read" || (action == "set" && r.opt.flag("execute")) {
		if e := r.ensureVehicle(); e != nil {
			return e
		}
	}
	groups := opRows(catalog["groups"])
	if body := r.opt.value("body-type", ""); body != "" {
		id, e := strconv.ParseInt(body, 0, 64)
		if e != nil {
			return e
		}
		filtered := []map[string]any{}
		for _, g := range groups {
			if opInt(g["body_type"]) == int(id) {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}
	if action == "groups" {
		if len(args) > 1 {
			groups = opMatch(groups, args[1], "group_id")
		}
		return r.operationRows(groups)
	}
	if len(args) < 2 {
		return fmt.Errorf("Customize %s requires GROUP", action)
	}
	matched := opMatch(groups, args[1], "group_id")
	ids := map[int]bool{}
	for _, g := range matched {
		ids[opInt(g["group_id"])] = true
	}
	if len(ids) != 1 {
		return fmt.Errorf("Customize group %q resolves %d groups", args[1], len(ids))
	}
	var gid int
	for id := range ids {
		gid = id
	}
	rows := []map[string]any{}
	for _, item := range opRows(catalog["items"]) {
		if opInt(item["group_id"]) == gid {
			rows = append(rows, item)
		}
	}
	if action == "items" {
		if len(args) > 2 {
			rows = opMatch(rows, args[2], "item_id")
		}
		return r.operationRows(rows)
	}
	if len(args) < 3 {
		return fmt.Errorf("Customize %s requires GROUP ITEM", action)
	}
	matches := opMatch(rows, args[2], "item_id")
	if len(matches) > 1 {
		installed := []map[string]any{}
		for _, m := range matches {
			for _, ecu := range r.profile.ECUs {
				if ecu.CategoryID == opInt(m["target_category_id"]) {
					installed = append(installed, m)
					break
				}
			}
		}
		if len(installed) > 0 {
			matches = installed
		}
	}
	if len(matches) != 1 {
		return fmt.Errorf("Customize item %q resolves %d matches", args[2], len(matches))
	}
	item := matches[0]
	if action == "info" {
		if r.opt.json {
			return r.output(item)
		}
		fmt.Printf("%s  ·  group %v / item %v\n", opText(item["name"]), item["group_id"], item["item_id"])
		fmt.Printf("  Target: %s (category %v), DID 0x%04X\n", opText(item["target_category_name"]), item["target_category_id"], opInt(item["write_did"]))
		for _, choice := range opRows(item["choices"]) {
			fmt.Printf("  %v  %s\n", choice["value"], opText(choice["name"]))
		}
		return nil
	}
	if action != "read" && action != "set" {
		return fmt.Errorf("unknown Customize command %q", action)
	}
	var targets []diagnostic.EcuSpec
	for _, ecu := range r.profile.ECUs {
		if ecu.CategoryID == opInt(item["target_category_id"]) {
			targets = append(targets, ecu)
		}
	}
	if len(targets) != 1 {
		return fmt.Errorf("Customize target resolves %d installed ECUs", len(targets))
	}
	ecu := targets[0]
	var phases []int
	for _, candidate := range r.profile.Candidates {
		if opInt(candidate["category_id"]) == ecu.CategoryID {
			phases = append(phases, opInt(candidate["connection_phase_type"]))
		}
	}
	if len(phases) != 1 || phases[0] != opInt(item["target_phase_type"]) {
		return fmt.Errorf("Customize target phase is ambiguous or disagrees with selected vehicle")
	}
	var value uint64
	var name string
	if action == "set" {
		if len(args) < 4 {
			return fmt.Errorf("Customize set requires VALUE")
		}
		value, name, e = diagnostic.ResolveCustomizeChoice(item, args[3])
		if e != nil {
			return e
		}
		if !r.opt.flag("execute") {
			if r.opt.json {
				return r.output(map[string]any{"executed": false, "item": item, "value": value, "choice": name})
			}
			fmt.Printf("%s → %s (%d)\nPlan only. Pass --execute to write and verify.\n", opText(item["name"]), name, value)
			return nil
		}
	}
	s, e := r.session(ecu)
	if e != nil {
		return e
	}
	support, _, e := r.opSupport(ecu, s, false)
	if e != nil {
		return e
	}
	if e := s.EnterExtended(r.ctx); e != nil {
		return e
	}
	if action == "read" {
		result, e := diagnostic.ReadCustomize(r.ctx, s.Client, item, support.Supports)
		if e != nil {
			return e
		}
		if e := s.Close(r.ctx); e != nil {
			return fmt.Errorf("session cleanup: %w", e)
		}
		return r.operationResult(result)
	}
	result, e := diagnostic.SetCustomize(r.ctx, s.Client, item, value, support.Supports)
	if e != nil {
		return e
	}
	if e := s.Close(r.ctx); e != nil {
		return fmt.Errorf("session cleanup: %w", e)
	}
	return r.operationResult(result)
}

func (r *runtimeApp) operationPlan(ecu diagnostic.EcuSpec, p diagnostic.TestPlan, group bool) error {
	materializable := diagnostic.CanMaterializeDirect(p) || diagnostic.CanMaterializeRoutine(p) || (group && len(p.Refusals) == 1 && p.Refusals[0] == "runtime payload length not definitively recovered")
	if r.opt.json {
		return r.output(map[string]any{"ecu": ecu.Key, "executed": false, "plan": p, "materializable": materializable})
	}
	state := "blocked"
	if p.Executable() {
		state = "executable"
	} else if materializable {
		state = "materializable"
	}
	fmt.Printf("%s  0x%04X  %s\n  %s · %s · session %s\n", ecu.Key, p.ID, p.Name, p.Kind, state, p.SessionRequirement)
	if len(p.Start) > 0 {
		fmt.Printf("  Start: %x\n", p.Start)
	}
	if len(p.Stop) > 0 {
		fmt.Printf("  Stop:  %x\n", p.Stop)
	}
	if len(p.Status) > 0 {
		fmt.Printf("  Poll:  %x\n", p.Status)
	}
	if p.Kind == "direct" {
		fmt.Printf("  DID: 0x%04X · bits %v..%v\n", p.Identifier, p.Row["bit_start"], p.Row["bit_end"])
	}
	for _, reason := range p.Refusals {
		fmt.Printf("  %s\n", reason)
	}
	fmt.Println("\nPlan only. No requests sent.")
	return nil
}
func (r *runtimeApp) operationRows(rows []map[string]any) error {
	if r.opt.json {
		return r.output(rows)
	}
	if len(rows) == 0 {
		fmt.Println("No matches.")
		return nil
	}
	for _, row := range rows {
		owner := opText(row["ecu"])
		if item := opMap(row["item"]); item != nil {
			row = item
		}
		id := row["id"]
		if id == nil {
			id = row["item_id"]
		}
		if id == nil {
			id = row["group_id"]
		}
		if id == nil {
			id = row["role"]
		}
		name := opText(row["name"])
		if name == "" {
			name = opText(row["semantic_kind"])
		}
		if name == "" {
			name = opText(row["dll"])
		}
		fmt.Printf("%-16s %-8v %-12s %s\n", owner, id, opText(row["execution"]), name)
	}
	return nil
}
func (r *runtimeApp) operationResult(result any) error {
	if r.opt.json {
		return r.output(result)
	}
	switch v := result.(type) {
	case diagnostic.ActiveTestResult:
		fmt.Printf("0x%04X %s: completed\n  Start: %x\n  Stop:  %x\n", v.Plan.ID, v.Plan.Name, v.Start, v.Stop)
		if len(v.Statuses) > 0 {
			fmt.Printf("  %d status samples\n", len(v.Statuses))
		}
	case diagnostic.CustomizeValue:
		fmt.Printf("%s (raw %d, payload %x)\n", v.Choice, v.Value, v.Payload)
	case diagnostic.CustomizeResult:
		state := "already set"
		if v.Changed {
			state = "verified"
		}
		fmt.Printf("%s (%d) → %s (%d): %s\n", v.Before.Choice, v.Before.Value, v.After.Choice, v.After.Value, state)
	default:
		return r.output(result)
	}
	return nil
}
