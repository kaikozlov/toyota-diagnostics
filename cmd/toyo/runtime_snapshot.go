package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
)

func snapshotNegative(err error) bool {
	var n *transport.NegativeResponseError
	return errors.As(err, &n)
}

func (r *runtimeApp) readVIN() (map[string]any, error) {
	if err := r.connect(false); err != nil {
		return nil, err
	}
	buses := []int{0, 1}
	if r.opt.value("bus", "") != "" || r.opt.value("transport", "panda") == "j2534" {
		buses = []int{r.profile.Bus}
	}
	// VIN queries are read-only standard physical OBD/UDS requests. Try both
	// addressing formats; ISO-TP owns flow control and response reassembly.
	addresses := []uint32{0x7e0, 0x7e2, 0x18da10f1, 0x7e1, 0x7e3, 0x7e4, 0x7e5, 0x7e6, 0x7e7}
	for attempt := 0; attempt < 2; attempt++ {
		for _, bus := range buses {
			for _, tx := range addresses {
				if err := r.ctx.Err(); err != nil {
					return nil, err
				}
				rx := tx + 8
				if tx > 0x7ff {
					rx = (tx & 0x1fff0000) | ((tx & 0xff) << 8) | ((tx >> 8) & 0xff)
				}
				c := &transport.Client{CAN: r.transport, TxID: tx, RxID: rx, Bus: bus, Timeout: 100 * time.Millisecond, PendingTimeout: 2 * time.Second}
				for _, q := range [][]byte{{0x22, 0xf1, 0x90}, {9, 2}} {
					response, err := c.Raw(r.ctx, q)
					if err != nil {
						continue
					}
					var data []byte
					if bytes.HasPrefix(response, []byte{0x62, 0xf1, 0x90}) {
						data = response[3:]
					} else if bytes.HasPrefix(response, []byte{0x49, 2, 1}) {
						data = response[3:]
					}
					vin := strings.TrimRight(string(data), "\x00")
					if diagnostic.ValidVIN(vin) {
						return map[string]any{"vin": vin, "rx_address": rx, "rx_bus": bus}, nil
					}
				}
			}
		}
	}
	// Broadcast fallback demultiplexes all responders and sends flow control
	// on each responder's physical route, never the functional address.
	for _, bus := range buses {
		result, err := transport.FunctionalVIN(r.ctx, r.transport, bus, 100*time.Millisecond)
		if err == nil {
			return map[string]any{"vin": result.VIN, "rx_address": result.Address, "rx_bus": result.Bus}, nil
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
	}
	return nil, fmt.Errorf("could not obtain a valid 17-character VIN on buses %v; select --vehicle TYPE explicitly", buses)
}

// vinSections reads the regional index sections resolveVINRows uses.
func (r *runtimeApp) vinSections() (map[string]any, error) {
	regional := map[string]any{}
	for _, name := range []string{"vin_decision", "routes", "selected_categories", "vehicle_resolver_dispatch", "vehicles"} {
		value, err := r.profile.Section(r.ctx, name)
		if err != nil {
			return nil, err
		}
		regional[name] = value
	}
	return regional, nil
}

func resolveVINRows(regional map[string]any, vin string, rx uint32) ([]map[string]any, error) {
	if !diagnostic.ValidVIN(vin) {
		return nil, fmt.Errorf("invalid VIN %q", vin)
	}
	rows := runtimeRows(runtimeMap(regional["vin_decision"])["rows"])
	if rows == nil {
		return nil, fmt.Errorf("catalog has no VIN decision rows")
	}
	keys := map[string]bool{}
	for key, value := range runtimeMap(regional["routes"]) {
		route := runtimeMap(value)
		request := runtimeNumber(route["legislated_request_address"])
		if request == 0 {
			continue
		}
		response := request + 8
		if request > 0x7ff {
			response = (request & 0x1fff0000) | ((request & 255) << 8) | ((request >> 8) & 255)
		}
		if uint32(response) == rx {
			keys[key] = true
		}
	}
	selectRows := func(filter bool) []map[string]any {
		out := []map[string]any{}
		for _, row := range rows {
			category := runtimeNumber(row["category_id"])
			selector := row["route_selector"]
			if selector == nil {
				selector = row["phase_type"]
			}
			phase := runtimeNumber(selector)
			if filter && !keys[fmt.Sprintf("%d:%d", category, phase)] {
				continue
			}
			match, e := diagnostic.VINDecisionMatches(row, vin, category, phase)
			if e == nil && match {
				out = append(out, row)
			}
		}
		return out
	}
	matched := selectRows(len(keys) > 0)
	if len(matched) == 0 && len(keys) > 0 {
		matched = selectRows(false)
	}
	categories := runtimeMap(regional["selected_categories"])
	dispatch := runtimeMap(regional["vehicle_resolver_dispatch"])
	phases := runtimeMap(dispatch["vin10_generation_low5"])
	vehicles := runtimeMap(regional["vehicles"])
	byType := map[int][]map[string]any{}
	stages := map[int]map[string]bool{}
	for _, row := range matched {
		id := runtimeNumber(row["vehicle_type"])
		cat := runtimeMap(categories[strconv.Itoa(runtimeNumber(row["category_id"]))])
		generation := runtimeNumber(cat["generation_low5"])
		if cat["generation_low5"] == nil {
			generation = runtimeNumber(cat["generation"]) & 31
		}
		phase := phases[strconv.Itoa(generation)]
		stage := "resolver_path_unresolved"
		if phase == "phase5" || phase == "phase6" {
			stage = "vin_final"
		} else if phase == "phase3" || phase == "phase4" {
			stage = "requires_type41_vehicle_decision"
		} else {
			if list, ok := dispatch["vin10_rejected_generation_low5"].([]any); ok {
				for _, v := range list {
					if runtimeNumber(v) == generation {
						stage = "requires_legacy_vehicle_selector"
					}
				}
			}
		}
		enriched := map[string]any{}
		for k, v := range row {
			enriched[k] = v
		}
		enriched["resolver_stage"] = stage
		byType[id] = append(byType[id], enriched)
		if stages[id] == nil {
			stages[id] = map[string]bool{}
		}
		stages[id][stage] = true
	}
	ids := []int{}
	for id := range byType {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := []map[string]any{}
	for _, id := range ids {
		v := runtimeMap(vehicles[strconv.Itoa(id)])
		if v == nil {
			continue
		}
		row := map[string]any{}
		for k, value := range v {
			row[k] = value
		}
		stageNames := []string{}
		for s := range stages[id] {
			stageNames = append(stageNames, s)
		}
		sort.Strings(stageNames)
		row["decision_rows"] = byType[id]
		row["resolver_stages"] = stageNames
		row["resolution_complete"] = len(stageNames) == 1 && stageNames[0] == "vin_final"
		out = append(out, row)
	}
	return out, nil
}
func (r *runtimeApp) ensureVehicle() error {
	if r.profile.Vehicle != nil {
		return nil
	}
	info, err := r.readVIN()
	if err != nil {
		return err
	}
	regional, err := r.vinSections()
	if err != nil {
		return err
	}
	rows, err := resolveVINRows(regional, fmt.Sprint(info["vin"]), uint32(runtimeNumber(info["rx_address"])))
	if err != nil {
		return err
	}
	complete := []map[string]any{}
	for _, row := range rows {
		if row["resolution_complete"] == true {
			complete = append(complete, row)
		}
	}
	if len(complete) != 1 {
		return fmt.Errorf("VIN %s resolved %d complete vehicle candidates (%d total); select --vehicle TYPE explicitly", info["vin"], len(complete), len(rows))
	}
	bus := r.profile.Bus
	if r.opt.value("bus", "") == "" {
		bus = runtimeNumber(info["rx_bus"])
	}
	r.profile, err = diagnostic.NewProfile(r.ctx, r.db, r.profile.Region, strconv.Itoa(runtimeNumber(complete[0]["vehicle_type"])), bus)
	return err
}

func (r *runtimeApp) mounted() ([]map[string]any, error) {
	if err := r.ensureVehicle(); err != nil {
		return nil, err
	}
	rows := []map[string]any{}
	cache := map[string]map[string]any{}
	for _, candidate := range r.profile.Candidates {
		row := map[string]any{}
		for k, v := range candidate {
			row[k] = v
		}
		row["transport_responded"] = nil
		row["probe_available"] = false
		row["support_root"] = nil
		row["supported_group_count"] = nil
		raw := runtimeMap(candidate["transport_route"])
		if raw == nil {
			row["live_state"] = "route_unresolved"
			row["probe_error"] = "Toyota class-0x10D route is not resolved in this corpus"
			rows = append(rows, row)
			continue
		}
		route, err := diagnostic.RouteForCandidate(candidate)
		if err != nil {
			return nil, err
		}
		family, mode := fmt.Sprint(candidate["support_family"]), fmt.Sprint(candidate["support_mode"])
		if mode != "p5-standard" && mode != "p6-standard" || !route.UDSTransportSupported() {
			row["live_state"] = "probe_unavailable"
			row["probe_error"] = fmt.Sprintf("exact support executor for %s/%s over %s is not known", family, mode, route.TransportKind)
			rows = append(rows, row)
			continue
		}
		contract := runtimeMap(runtimeMap(r.profile.Index["support_contracts"])[family])
		key := fmt.Sprintf("%x/%x/%s", route.RequestAddress, route.AddressExtension, mode)
		state := cache[key]
		if state == nil {
			ecu := diagnostic.EcuSpec{Key: route.Name, Address: uint32(route.RequestAddress), CategoryID: route.CategoryID, RouteResolved: true, TransportKind: route.TransportKind}
			if route.AddressExtension != 0 {
				v := byte(route.AddressExtension)
				ecu.SubAddr = &v
			}
			client, e := r.client(ecu)
			if e != nil {
				return nil, e
			}
			resolver, e := diagnostic.NewSupportResolver(client, contract, mode, false)
			if e != nil {
				row["live_state"] = "probe_unavailable"
				row["probe_error"] = e.Error()
				rows = append(rows, row)
				continue
			}
			state = map[string]any{"probe_available": true, "support_root_did": resolver.Root}
			groups, e := resolver.SupportedGroups(r.ctx)
			if e == nil {
				state["live_state"] = "responding"
				state["transport_responded"] = true
				state["support_root"] = true
				state["supported_group_count"] = len(groups)
			} else if snapshotNegative(e) {
				state["live_state"] = "responding"
				state["transport_responded"] = true
				state["support_root"] = false
				state["support_error"] = e.Error()
			} else if isTimeout(e) {
				state["live_state"] = "no_response"
				state["transport_responded"] = false
				state["support_error"] = e.Error()
			} else {
				return nil, e
			}
			cache[key] = state
		}
		for k, v := range state {
			row[k] = v
		}
		rows = append(rows, row)
	}
	return rows, nil
}

func (r *runtimeApp) snapshotCommands(args []string) error {
	switch args[0] {
	case "vehicle":
		if len(args) < 2 {
			return usagef("vehicle", "expected detect or mounted")
		}
		if args[1] == "show" {
			if r.profile.Vehicle == nil {
				return r.output(map[string]any{"region": r.profile.Region, "vehicle": nil, "message": "no vehicle selected; use --vehicle TYPE"})
			}
			return r.output(map[string]any{"region": r.profile.Region, "vehicle": r.profile.Vehicle, "logical_bus": r.profile.Bus, "ecus": r.profile.ECUs})
		}
		if args[1] == "mounted" {
			rows, e := r.mounted()
			if e != nil {
				return e
			}
			summary := map[string]any{"vehicle_type": r.profile.Vehicle["vehicle_type"], "vehicle_name": r.profile.Vehicle["name"], "install_set_ids": r.profile.Vehicle["install_set_ids"], "candidate_count": len(rows), "responding": 0, "no_response": 0, "probe_unavailable": 0, "route_unresolved": 0, "candidates": rows}
			for _, row := range rows {
				key := fmt.Sprint(row["live_state"])
				summary[key] = runtimeNumber(summary[key]) + 1
			}
			return r.output(summary)
		}
		if args[1] == "detect" {
			var info map[string]any
			var e error
			if vin := r.opt.value("vin", ""); vin != "" {
				info = map[string]any{"vin": vin}
			} else {
				info, e = r.readVIN()
				if e != nil {
					return e
				}
			}
			regional, e := r.vinSections()
			if e != nil {
				return e
			}
			rows, e := resolveVINRows(regional, fmt.Sprint(info["vin"]), uint32(runtimeNumber(info["rx_address"])))
			if e != nil {
				return e
			}
			info["matches"] = rows
			return r.output(info)
		}
		return usagef("vehicle", "unknown action %q", args[1])
	case "health-check", "scan":
		if len(args) > 1 && args[1] == "diff" {
			if len(args) != 4 {
				return usagef("health-check", "diff BEFORE AFTER")
			}
			a, e := diagnostic.LoadSnapshot(args[2])
			if e != nil {
				return e
			}
			b, e := diagnostic.LoadSnapshot(args[3])
			if e != nil {
				return e
			}
			d, e := diagnostic.CompareSnapshots(a, b)
			if e != nil {
				return e
			}
			if r.opt.json {
				return r.output(d)
			}
			fmt.Fprintln(os.Stdout, diagnostic.RenderSnapshotDiff(d))
			return nil
		}
		if len(args) > 1 && args[1] == "show" {
			if len(args) != 3 {
				return usagef("health-check", "show SNAPSHOT")
			}
			d, e := diagnostic.LoadSnapshot(args[2])
			if e != nil {
				return e
			}
			if r.opt.json {
				return r.output(d)
			}
			fmt.Fprintln(os.Stdout, diagnostic.RenderSnapshot(d))
			return nil
		}
		return r.healthCheck()
	case "ffd":
		return r.recorderCommand(args[1:])
	}
	return usagef(args[0], "unknown command")
}
func (r *runtimeApp) healthCheck() error {
	var before map[string]any
	if path := r.opt.value("compare", ""); path != "" {
		var err error
		before, err = diagnostic.LoadSnapshot(path)
		if err != nil {
			return err
		}
	}
	mounts, e := r.mounted()
	if e != nil {
		return e
	}
	ecus := []map[string]any{}
	for _, ecu := range r.profile.ECUs {
		var mount map[string]any
		for _, m := range mounts {
			if runtimeNumber(m["category_id"]) == ecu.CategoryID {
				route, err := diagnostic.RouteForCandidate(m)
				if err == nil && uint32(route.RequestAddress) == ecu.Address {
					mount = m
					break
				}
			}
		}
		var subAddress any
		if ecu.SubAddr != nil {
			subAddress = int(*ecu.SubAddr)
		}
		row := map[string]any{"key": ecu.Key, "name": ecu.Name, "category_id": ecu.CategoryID, "generation": ecu.Generation, "address": ecu.Address, "sub_addr": subAddress, "transport_kind": ecu.TransportKind, "mount": mount, "identity": nil}
		dtc := map[string]any{"state": "not_queried", "records": []map[string]any{}, "fault_count": 0}
		ffd := map[string]any{"state": "not_queried", "dtcs": []map[string]any{}, "record_count": 0}
		rob := map[string]any{"state": "not_queried", "groups": []map[string]any{}}
		if mount != nil && mount["transport_responded"] == true {
			client, err := r.client(ecu)
			if err != nil {
				return err
			}
			payload, err := client.Request(r.ctx, 0x19, []byte{2, 255})
			dtc["state"] = "positive"
			if err != nil {
				dtc["state"] = "error"
				if isTimeout(err) {
					dtc["state"] = "no_response"
				} else if snapshotNegative(err) {
					dtc["state"] = "negative_response"
				}
				dtc["error"] = err.Error()
			} else if len(payload) == 0 || payload[0] != 2 {
				dtc["state"] = "error"
				dtc["error"] = "DTC subfunction echo mismatch"
			} else {
				parsed, err := diagnostic.ParseDTCResponse(payload[1:])
				if err != nil {
					dtc["state"] = "error"
					dtc["error"] = err.Error()
				} else {
					records := []map[string]any{}
					faults := 0
					for _, d := range parsed {
						bits, _ := diagnostic.DecodeStatusBits(int(d.Status))
						mask := 0xaf
						if r.profile.Index["fault_status_mask"] != nil {
							mask = runtimeNumber(r.profile.Index["fault_status_mask"])
						}
						fault := int(d.Status)&mask != 0
						if fault {
							faults++
						}
						records = append(records, map[string]any{"code": d.Code, "status": d.Status, "status_bits": bits, "fault_status": fault, "descriptions": []any{}})
					}
					dtc["records"] = records
					dtc["fault_count"] = faults
				}
			}
			catalog, err := r.profile.Category(r.ctx, ecu)
			if err != nil {
				return err
			}
			for _, record := range runtimeRows(dtc["records"]) {
				descriptions := []map[string]any{}
				for _, rows := range runtimeMap(catalog["dtcs"]) {
					for _, description := range runtimeRows(rows) {
						normalize := func(v any) string {
							return strings.ToLower(strings.NewReplacer("-", "", " ", "", "_", "").Replace(fmt.Sprint(v)))
						}
						if normalize(description["code"]) == normalize(record["code"]) {
							descriptions = append(descriptions, description)
						}
					}
				}
				record["descriptions"] = descriptions
			}
			identityPlan, did := diagnostic.GenericCIDPlan(catalog)
			ffdPlan, robPlan := diagnostic.P5SnapshotPlan(catalog), diagnostic.P5RoBPlan(catalog)
			if r.opt.flag("no-identity") {
				identityPlan = nil
			}
			if ffdPlan == nil {
				ffd = map[string]any{"state": "not_available", "dtcs": []any{}, "positive_dtc_count": 0, "record_count": 0}
			}
			if robPlan == nil {
				rob = map[string]any{"state": "not_available", "groups": []any{}, "behavior_code_count": 0, "record_count": 0}
			}
			if identityPlan != nil || ffdPlan != nil || robPlan != nil {
				session, err := r.session(ecu)
				needsExtended := false
				for _, plan := range []map[string]any{identityPlan, ffdPlan, robPlan} {
					for _, request := range runtimeRows(plan["requests"]) {
						if request["session_requirement"] == "extended" {
							needsExtended = true
						}
					}
					for _, protocol := range runtimeRows(plan["protocols"]) {
						for _, phase := range []string{"inventory", "frames", "record"} {
							if runtimeMap(protocol[phase])["session_requirement"] == "extended" {
								needsExtended = true
							}
						}
					}
				}
				if needsExtended && err == nil {
					err = session.EnterExtended(r.ctx)
				}
				if err != nil {
					if ffdPlan != nil {
						ffd = map[string]any{"state": "error", "error": err.Error(), "dtcs": []any{}}
					}
					if robPlan != nil {
						rob = map[string]any{"state": "error", "error": err.Error(), "groups": []any{}}
					}
					if identityPlan != nil {
						row["identity"] = map[string]any{"did": did, "state": "error", "error": err.Error(), "data_hex": nil, "ascii": nil}
					}
				} else {
					if identityPlan != nil && !r.opt.flag("no-identity") {
						row["identity"] = diagnostic.SnapshotIdentity(r.ctx, session.Client, did)
					}
					if dtc["state"] == "positive" {
						ffd = diagnostic.SnapshotFreezeFrames(r.ctx, session.Client, ffdPlan, runtimeRows(dtc["records"]), runtimeMap(catalog["generic_ffd"]))
					}
					rob = diagnostic.SnapshotRoB(r.ctx, session.Client, robPlan, runtimeMap(catalog["rob"]))
				}
				if session != nil {
					if err = session.Close(r.ctx); err != nil {
						ffd["cleanup_errors"] = session.CleanupErrors
						rob["cleanup_errors"] = session.CleanupErrors
						if identity := runtimeMap(row["identity"]); identity != nil {
							identity["cleanup_errors"] = session.CleanupErrors
						}
					}
				}
			}
		}
		row["dtc"] = dtc
		row["freeze_frames"] = ffd
		row["rob"] = rob
		ecus = append(ecus, row)
	}
	d := diagnostic.NewSnapshot("toyota-"+strings.ToLower(r.profile.Region), fmt.Sprint(r.profile.Vehicle["name"]), r.profile.Region, runtimeNumber(r.profile.Vehicle["vehicle_type"]), r.profile.Bus, map[string]any{"backend": r.opt.value("transport", "panda"), "logical_bus": r.profile.Bus}, ecus, mounts)
	if path := r.opt.value("save", ""); path != "" {
		if e := diagnostic.SaveSnapshot(d, path); e != nil {
			return e
		}
	}
	var diff map[string]any
	if before != nil {
		diff, e = diagnostic.CompareSnapshots(before, d)
		if e != nil {
			return e
		}
	}
	if r.opt.json {
		var value any = d
		if diff != nil {
			value = map[string]any{"snapshot": d, "diff": diff}
		}
		if e := r.output(value); e != nil {
			return e
		}
		if runtimeNumber(runtimeMap(d["summary"])["fault_status_records"]) > 0 {
			return exitStatus(1)
		}
		return nil
	}
	fmt.Fprintln(os.Stdout, diagnostic.RenderSnapshot(d))
	if diff != nil {
		fmt.Fprintln(os.Stdout, diagnostic.RenderSnapshotDiff(diff))
	}
	if path := r.opt.value("save", ""); path != "" {
		fmt.Fprintln(os.Stdout, "saved:", path)
	}
	if runtimeNumber(runtimeMap(d["summary"])["fault_status_records"]) > 0 {
		return exitStatus(1)
	}
	return nil
}
func (r *runtimeApp) recorderCommand(args []string) error {
	if len(args) == 0 {
		return usagef("ffd", "expected data, robs, operation or image")
	}
	semantics, e := r.db.Document(r.ctx, "supplement://pcs_data_viewer_tss3_managed_semantics.json")
	if e != nil {
		return fmt.Errorf("recorder semantics missing from catalog: %w", e)
	}
	signals := runtimeRows(runtimeMap(semantics["operation_ffd"])["detail_rows"])
	robs := runtimeRows(runtimeMap(semantics["rob_codes"])["rows"])
	if args[0] == "data" || args[0] == "robs" {
		rows := signals
		if args[0] == "robs" {
			rows = robs
		}
		query := ""
		if len(args) > 1 {
			query = strings.ToLower(strings.Join(args[1:], " "))
		}
		out := []map[string]any{}
		limit, err := runtimeInt(r.opt.value("limit", "100"))
		if err != nil || limit < 0 {
			return fmt.Errorf("--limit must be nonnegative")
		}
		total := 0
		for _, row := range rows {
			hay := strings.ToLower(fmt.Sprint(row["DataID"], " ", row["DataName"], " ", row["SystemName"], " ", row["rob_code"]))
			match := true
			for _, word := range strings.Fields(query) {
				if !strings.Contains(hay, word) {
					match = false
				}
			}
			if match {
				total++
				if limit == 0 || len(out) < limit {
					item := map[string]any{}
					for k, v := range row {
						item[k] = v
					}
					idKey, outKey := "DataID", "data_id"
					if args[0] == "robs" {
						idKey, outKey = "rob_code", "rob"
					}
					n, _ := strconv.ParseUint(strings.TrimPrefix(fmt.Sprint(row[idKey]), "0x"), 16, 32)
					item[outKey] = n
					out = append(out, item)
				}
			}
		}
		key := "signals"
		if args[0] == "robs" {
			key = "robs"
		}
		if r.opt.json {
			if err := r.output(map[string]any{"total": total, key: out}); err != nil {
				return err
			}
		} else {
			for _, row := range out {
				fmt.Fprintf(os.Stdout, "%v  %v\n", row["DataID"], row["DataName"])
			}
			if total > len(out) {
				fmt.Fprintf(os.Stdout, "... %d more; raise --limit\n", total-len(out))
			}
		}
		if total == 0 {
			return exitStatus(1)
		}
		return nil
	}
	if len(args) < 2 {
		return usagef("ffd", "expected operation/image action")
	}
	expected := 0
	switch args[0] + "/" + args[1] {
	case "operation/list", "image/info", "image/list":
		expected = 2
	case "operation/records":
		expected = 3
	case "operation/read", "image/read":
		expected = 4
	default:
		return usagef("ffd", "unknown action %q", strings.Join(args, " "))
	}
	if len(args) != expected {
		return usagef("ffd", "wrong number of recorder identifiers")
	}
	for i, s := range args[2:] {
		n, err := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X"), 16, 64)
		maximum := uint64(65535)
		if args[0] == "image" && i == 1 {
			maximum = 0xffffffff
		}
		if err != nil || n > maximum {
			return fmt.Errorf("invalid recorder identifier %q", s)
		}
	}
	if e := r.ensureVehicle(); e != nil {
		return e
	}
	ecu, e := r.ecu(r.opt.value("ecu", "frc"))
	if e != nil {
		return e
	}
	client, e := r.client(ecu)
	if e != nil {
		return e
	}
	word := func(s string, maxValue uint64) (uint64, error) {
		n, e := strconv.ParseUint(strings.TrimPrefix(strings.TrimPrefix(strings.TrimSpace(s), "0x"), "0X"), 16, 64)
		if e != nil || n > maxValue {
			return 0, fmt.Errorf("invalid recorder identifier %q", s)
		}
		return n, nil
	}
	if args[0] == "operation" {
		switch args[1] {
		case "list":
			response, e := client.Raw(r.ctx, []byte{0xab, 0x11})
			if e != nil {
				return e
			}
			codes, e := diagnostic.ParseOperationBehaviors(response)
			if e != nil {
				return e
			}
			return r.output(map[string]any{"behaviors": codes, "total": len(codes)})
		case "records":
			if len(args) != 3 {
				return usagef("ffd", "operation records BEHAVIOR")
			}
			id, e := word(args[2], 65535)
			if e != nil {
				return e
			}
			response, e := client.Raw(r.ctx, []byte{0xab, 0x12, byte(id >> 8), byte(id)})
			if e != nil {
				return e
			}
			records, e := diagnostic.ParseOperationRecords(response, int(id))
			if e != nil {
				return e
			}
			return r.output(map[string]any{"behavior": id, "records": records, "total": len(records)})
		case "read":
			if len(args) != 4 {
				return usagef("ffd", "operation read BEHAVIOR RECORD")
			}
			id, e := word(args[2], 65535)
			if e != nil {
				return e
			}
			record, e := word(args[3], 65535)
			if e != nil {
				return e
			}
			response, e := client.Raw(r.ctx, []byte{0xab, 0x13, byte(id >> 8), byte(id), byte(record >> 8), byte(record)})
			if e != nil {
				return e
			}
			parsed, e := diagnostic.ParseOperationRecord(response, int(id), int(record))
			if e != nil {
				return e
			}
			blocks := []map[string]any{}
			for _, block := range parsed.Blocks {
				rows := []map[string]any{}
				for _, signal := range signals {
					did, _ := strconv.ParseUint(fmt.Sprint(signal["DataID"]), 16, 16)
					if int(did) == block.DataID {
						rows = append(rows, signal)
					}
				}
				decoded := diagnostic.DecodeRecorderBlock(block, rows)
				query := strings.ToLower(r.opt.value("query", ""))
				match := query == "" || strings.Contains(fmt.Sprintf("%04x", block.DataID), strings.TrimPrefix(query, "0x"))
				for _, signal := range runtimeRows(decoded["signals"]) {
					if strings.Contains(strings.ToLower(fmt.Sprint(signal["name"])), query) {
						match = true
					}
				}
				if match {
					blocks = append(blocks, decoded)
				}
			}
			return r.output(map[string]any{"behavior": id, "record": record, "block_count": parsed.BlockCount, "blocks": blocks})
		}
	}
	if args[0] == "image" {
		response, e := client.Request(r.ctx, 0x10, []byte{3})
		if e != nil {
			return e
		}
		if len(response) == 0 || response[0] != 3 {
			return fmt.Errorf("extended session echo mismatch")
		}
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if _, e := client.Request(ctx, 0x10, []byte{1}); e != nil {
				fmt.Fprintln(os.Stderr, "image session cleanup:", e)
			}
		}()
		info := map[string]any{}
		if args[1] == "info" {
			for _, field := range []struct {
				key string
				did uint16
			}{{"spec_information_hex", 0x1103}, {"availability_hex", 0x1101}} {
				key, did := field.key, field.did
				b, e := diagnostic.ReadDID(r.ctx, client, did)
				if e != nil {
					return e
				}
				info[key] = hex.EncodeToString(b)
			}
		}
		seed, e := client.Request(r.ctx, 0x27, []byte{3})
		if e != nil {
			return e
		}
		if len(seed) != 7 || seed[0] != 3 {
			return fmt.Errorf("Image FFD SecurityAccess: expected 03 + 6-byte seed")
		}
		key, e := diagnostic.Level49Key(seed[1:])
		if e != nil {
			return e
		}
		response, e = client.Request(r.ctx, 0x27, append([]byte{4}, key...))
		if e != nil {
			return e
		}
		if len(response) == 0 || response[0] != 4 {
			return fmt.Errorf("Image FFD SecurityAccess key echo mismatch")
		}
		info["seed_hex"] = hex.EncodeToString(seed[1:])
		info["key_hex"] = hex.EncodeToString(key)
		switch args[1] {
		case "info":
			b, e := diagnostic.ReadDID(r.ctx, client, 0x2081)
			if e != nil {
				return e
			}
			info["encryption_method_hex"] = hex.EncodeToString(b)
			return r.output(info)
		case "list":
			response, e := client.Raw(r.ctx, []byte{0xab, 0x31})
			if e != nil {
				return e
			}
			ids, e := diagnostic.ParseImageRoBs(response)
			if e != nil {
				return e
			}
			return r.output(map[string]any{"robs": ids, "total": len(ids), "security": info})
		case "read":
			if len(args) != 4 {
				return usagef("ffd", "image read ROB FRAME")
			}
			rob, e := word(args[2], 65535)
			if e != nil {
				return e
			}
			frame, e := word(args[3], 0xffffffff)
			if e != nil {
				return e
			}
			q := []byte{0xab, 0x33, byte(rob >> 8), byte(rob), 0, 0, 0, 0}
			binary.BigEndian.PutUint32(q[4:], uint32(frame))
			response, e := client.Raw(r.ctx, q)
			if e != nil {
				return e
			}
			record, e := diagnostic.ParseImageRecord(response, int(rob), uint32(frame))
			if e != nil {
				return e
			}
			blocks := []map[string]any{}
			for _, b := range record.Blocks {
				blocks = append(blocks, map[string]any{"data_id": b.DataID, "length": b.Length, "data_hex": hex.EncodeToString(b.Data)})
			}
			return r.output(map[string]any{"rob": rob, "frame": frame, "block_count": record.BlockCount, "blocks": blocks})
		}
	}
	return usagef("ffd", "unknown action %q", strings.Join(args, " "))
}
