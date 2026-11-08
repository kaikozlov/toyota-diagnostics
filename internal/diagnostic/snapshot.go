package diagnostic

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"
)

const SnapshotSchema = "toyota-health-check-v1"

func SnapshotRoB(ctx context.Context, client RequestClient, command, metadata map[string]any) map[string]any {
	out := map[string]any{"state": "not_available", "groups": []map[string]any{}, "behavior_code_count": 0, "unique_behavior_code_count": 0, "frame_count": 0, "record_count": 0, "did_block_count": 0, "decoded_signal_count": 0, "suppressed_signal_count": 0, "decode_error_count": 0}
	if command == nil {
		return out
	}
	out["state"] = "available"
	signals := snapshotSignals(metadata, "did")
	out["signal_decode"] = "raw_only"
	if len(signals) > 0 {
		out["signal_decode"] = "gts_current_p5"
	}
	names := map[int]map[string]any{}
	for _, row := range resolveRows(metadata["behavior_codes"]) {
		id := snapshotInt(row["behavior_code"])
		if names[id] == nil {
			names[id] = row
		}
	}
	groups := []map[string]any{}
	unique := map[int]bool{}
	for _, protocol := range resolveRows(command["protocols"]) {
		inv := resolveMap(protocol["inventory"])
		frames := resolveMap(protocol["frames"])
		rec := resolveMap(protocol["record"])
		iq, e := resolveHex(inv["send"])
		ic, e2 := resolveHex(inv["check"])
		fq, e3 := resolveHex(frames["send"])
		fc, e4 := resolveHex(frames["check"])
		rq, e5 := resolveHex(rec["send"])
		rc, e6 := resolveHex(rec["check"])
		group := map[string]any{"state": "positive", "behavior_codes": []int{}, "behaviors": []map[string]any{}}
		if e != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || e6 != nil || len(iq) < 2 || len(fq) < 4 || len(rq) < 6 {
			group["state"] = "error"
			group["error"] = "invalid P5 RoB command bytes"
			groups = append(groups, group)
			continue
		}
		group["inventory_subfunction"] = int(iq[1])
		group["frame_subfunction"] = int(fq[1])
		group["record_subfunction"] = int(rq[1])
		response, e := snapshotRequest(ctx, client, iq)
		if e != nil {
			group["state"] = snapshotErrorState(e)
			group["error"] = e.Error()
			groups = append(groups, group)
			continue
		}
		group["inventory_response_hex"] = hex.EncodeToString(response)
		codes, e := ParseP5RoBBehaviorCodes(response, ic)
		if e != nil {
			group["state"] = "parse_error"
			group["error"] = e.Error()
			groups = append(groups, group)
			continue
		}
		group["behavior_codes"] = codes
		out["behavior_code_count"] = snapshotInt(out["behavior_code_count"]) + len(codes)
		behaviors := []map[string]any{}
		for _, code := range codes {
			unique[code] = true
			request := bytes.Clone(fq)
			binary.BigEndian.PutUint16(request[2:4], uint16(code))
			response, e = snapshotRequest(ctx, client, request)
			behavior := map[string]any{"behavior_code": code, "state": "positive", "frames": []map[string]any{}}
			if meta := names[code]; meta != nil {
				behavior["behavior_signature"] = meta["signature"]
				behavior["behavior_name"] = meta["name"]
				behavior["behavior_comment"] = meta["comment"]
			}
			if e != nil {
				behavior["state"] = snapshotErrorState(e)
				behavior["error"] = e.Error()
				behaviors = append(behaviors, behavior)
				continue
			}
			behavior["frame_response_hex"] = hex.EncodeToString(response)
			parsed, e := ParseP5RoBFrames(response, fc)
			if e != nil {
				behavior["state"] = "parse_error"
				behavior["error"] = e.Error()
				behaviors = append(behaviors, behavior)
				continue
			}
			behavior["behavior_echo"] = parsed["behavior_echo"]
			ids, _ := parsed["frames"].([]int)
			out["frame_count"] = snapshotInt(out["frame_count"]) + len(ids)
			frameRows := []map[string]any{}
			for _, id := range ids {
				request = bytes.Clone(rq)
				binary.BigEndian.PutUint16(request[2:4], uint16(code))
				binary.BigEndian.PutUint16(request[4:6], uint16(id))
				response, e = snapshotRequest(ctx, client, request)
				frame := map[string]any{"frame_id": id, "state": "positive"}
				if e != nil {
					frame["state"] = snapshotErrorState(e)
					frame["error"] = e.Error()
					frameRows = append(frameRows, frame)
					continue
				}
				frame["response_hex"] = hex.EncodeToString(response)
				record, e := ParseP5RoBRecord(response, rc)
				if e != nil {
					frame["state"] = "parse_error"
					frame["error"] = e.Error()
					frameRows = append(frameRows, frame)
					continue
				}
				blocks := []map[string]any{}
				for _, block := range record.Blocks {
					decoded := []map[string]any{}
					errs := []string{}
					suppressed := 0
					for _, row := range signals[block.DataID] {
						value, e := DecodeRoBSignal(block.Data, row)
						if e != nil {
							errs = append(errs, fmt.Sprintf("%v: %v", row["name"], e))
							continue
						}
						if value["state"] == "not_supported" {
							suppressed++
							continue
						}
						for _, key := range []string{"bit_start", "bit_end", "extraction_mode"} {
							value[key] = snapshotInt(row[key])
						}
						decoded = append(decoded, value)
					}
					state := "decoded"
					if len(signals[block.DataID]) == 0 {
						state = "no_schema"
					}
					if len(errs) > 0 {
						state = "decode_error"
						if len(decoded) > 0 {
							state = "partial"
						}
					}
					blocks = append(blocks, map[string]any{"did": block.DataID, "length": block.Length, "data_hex": hex.EncodeToString(block.Data), "signal_decode": state, "signals": decoded, "suppressed_signal_count": suppressed, "decode_errors": errs})
					out["decoded_signal_count"] = snapshotInt(out["decoded_signal_count"]) + len(decoded)
					out["suppressed_signal_count"] = snapshotInt(out["suppressed_signal_count"]) + suppressed
					out["decode_error_count"] = snapshotInt(out["decode_error_count"]) + len(errs)
				}
				frame["record"] = map[string]any{"behavior_echo": record.BehaviorEcho, "frame_echo": record.FrameEcho, "declared_block_count": record.DeclaredBlockCount, "block_count": record.BlockCount, "blocks": blocks}
				out["record_count"] = snapshotInt(out["record_count"]) + 1
				out["did_block_count"] = snapshotInt(out["did_block_count"]) + len(blocks)
				frameRows = append(frameRows, frame)
			}
			behavior["frames"] = frameRows
			behaviors = append(behaviors, behavior)
		}
		group["behaviors"] = behaviors
		groups = append(groups, group)
	}
	out["groups"] = groups
	out["unique_behavior_code_count"] = len(unique)
	return out
}

func SaveSnapshot(document map[string]any, path string) error {
	if strings.HasPrefix(path, "~/") {
		home, e := os.UserHomeDir()
		if e != nil {
			return e
		}
		path = filepath.Join(home, path[2:])
	}
	if e := os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	data, e := json.MarshalIndent(document, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(data, '\n'), 0644)
}
func LoadSnapshot(path string) (map[string]any, error) {
	if strings.HasPrefix(path, "~/") {
		home, e := os.UserHomeDir()
		if e != nil {
			return nil, e
		}
		path = filepath.Join(home, path[2:])
	}
	b, e := os.ReadFile(path)
	if e != nil {
		return nil, fmt.Errorf("cannot read Health Check snapshot %s: %w", path, e)
	}
	var doc map[string]any
	if e = json.Unmarshal(b, &doc); e != nil {
		return nil, e
	}
	if doc["schema"] != SnapshotSchema {
		return nil, fmt.Errorf("%s is not a %s document", path, SnapshotSchema)
	}
	return doc, nil
}
func snapshotInt(v any) int { n, _ := resolveInt(v); return n }
func snapshotKey(row map[string]any) string {
	sub := "-"
	if row["sub_addr"] != nil {
		sub = fmt.Sprintf("%03d", snapshotInt(row["sub_addr"]))
	}
	return fmt.Sprintf("%010d/%010d/%s", snapshotInt(row["category_id"]), snapshotInt(row["address"]), sub)
}

type snapshotIndexed struct {
	key    string
	fields map[string]any
	value  any
}

func snapshotIndex(rows []snapshotIndexed) map[string]snapshotIndexed {
	out := map[string]snapshotIndexed{}
	for _, r := range rows {
		out[r.key] = r
	}
	return out
}
func snapshotDelta(old, new []snapshotIndexed, valueKey, changedKey string) map[string]any {
	a, b := snapshotIndex(old), snapshotIndex(new)
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	ordered := []string{}
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	added, removed, changed := []map[string]any{}, []map[string]any{}, []map[string]any{}
	for _, k := range ordered {
		before, hasOld := a[k]
		after, hasNew := b[k]
		row := map[string]any{}
		source := after
		if !hasNew {
			source = before
		}
		for f, v := range source.fields {
			row[f] = v
		}
		if !hasOld {
			if valueKey != "" {
				row[valueKey] = after.value
			}
			added = append(added, row)
		} else if !hasNew {
			if valueKey != "" {
				row[valueKey] = before.value
			}
			removed = append(removed, row)
		} else if !reflect.DeepEqual(before.value, after.value) {
			row["before"] = before.value
			row["after"] = after.value
			changed = append(changed, row)
		}
	}
	if len(added)+len(removed)+len(changed) == 0 {
		return nil
	}
	out := map[string]any{"added": added, "removed": removed}
	if changedKey != "" {
		out[changedKey] = changed
	}
	return out
}
func snapshotDTCIndex(row map[string]any) []snapshotIndexed {
	out := []snapshotIndexed{}
	for _, r := range resolveRows(resolveMap(row["dtc"])["records"]) {
		code := resolveText(r["code"])
		if code != "" {
			out = append(out, snapshotIndexed{code, map[string]any{"code": code}, snapshotInt(r["status"])})
		}
	}
	return out
}
func snapshotFFDIndex(row map[string]any) []snapshotIndexed {
	out := []snapshotIndexed{}
	for _, d := range resolveRows(resolveMap(row["freeze_frames"])["dtcs"]) {
		if d["state"] != "positive" {
			continue
		}
		for _, rec := range resolveRows(d["records"]) {
			for _, id := range resolveRows(rec["identifiers"]) {
				if id["did"] == nil || id["data_hex"] == nil {
					continue
				}
				fields := map[string]any{"dtc": d["dtc"], "record_number": snapshotInt(rec["record_number"]), "did": snapshotInt(id["did"])}
				key := fmt.Sprintf("%s/%010d/%010d", d["dtc"], fields["record_number"], fields["did"])
				out = append(out, snapshotIndexed{key, fields, id["data_hex"]})
			}
		}
	}
	return out
}
func snapshotROBIndexes(row map[string]any) ([]snapshotIndexed, []snapshotIndexed) {
	codes, records := []snapshotIndexed{}, []snapshotIndexed{}
	for _, g := range resolveRows(resolveMap(row["rob"])["groups"]) {
		if g["state"] != "positive" {
			continue
		}
		sf := g["inventory_subfunction"]
		if sf == nil {
			sf = g["subfunction"]
		}
		sub := snapshotInt(sf)
		if ids, ok := g["behavior_codes"].([]any); ok {
			for _, id := range ids {
				code := snapshotInt(id)
				codes = append(codes, snapshotIndexed{fmt.Sprintf("%010d/%010d", sub, code), map[string]any{"subfunction": sub, "behavior_code": code}, true})
			}
		}
		if ids, ok := g["behavior_codes"].([]int); ok {
			for _, code := range ids {
				codes = append(codes, snapshotIndexed{fmt.Sprintf("%010d/%010d", sub, code), map[string]any{"subfunction": sub, "behavior_code": code}, true})
			}
		}
		for _, b := range resolveRows(g["behaviors"]) {
			for _, f := range resolveRows(b["frames"]) {
				if f["state"] != "positive" {
					continue
				}
				for _, block := range resolveRows(resolveMap(f["record"])["blocks"]) {
					if block["did"] == nil || block["data_hex"] == nil {
						continue
					}
					code, frame, did := snapshotInt(b["behavior_code"]), snapshotInt(f["frame_id"]), snapshotInt(block["did"])
					records = append(records, snapshotIndexed{fmt.Sprintf("%010d/%010d/%010d/%010d", sub, code, frame, did), map[string]any{"subfunction": sub, "behavior_code": code, "frame_id": frame, "did": did}, block["data_hex"]})
				}
			}
		}
	}
	return codes, records
}
func CompareSnapshots(before, after map[string]any) (map[string]any, error) {
	if before["schema"] != SnapshotSchema || after["schema"] != SnapshotSchema {
		return nil, fmt.Errorf("Health Check comparison requires two %s documents", SnapshotSchema)
	}
	a, b := map[string]map[string]any{}, map[string]map[string]any{}
	keys := map[string]bool{}
	for _, r := range resolveRows(before["ecus"]) {
		k := snapshotKey(r)
		a[k] = r
		keys[k] = true
	}
	for _, r := range resolveRows(after["ecus"]) {
		k := snapshotKey(r)
		b[k] = r
		keys[k] = true
	}
	ordered := []string{}
	for k := range keys {
		ordered = append(ordered, k)
	}
	sort.Strings(ordered)
	changes := []map[string]any{}
	summary := map[string]any{"changed_ecus": 0, "mount_state_changes": 0, "dtc_changes": 0, "freeze_frame_changes": 0, "rob_changes": 0, "identity_changes": 0}
	for _, k := range ordered {
		old, new := a[k], b[k]
		src := new
		if src == nil {
			src = old
		}
		entry := map[string]any{}
		for _, f := range []string{"category_id", "address", "sub_addr", "key", "name"} {
			entry[f] = src[f]
		}
		if old == nil {
			entry["change"] = "candidate_added"
		} else if new == nil {
			entry["change"] = "candidate_removed"
		} else {
			for _, f := range []struct{ key, parent, field string }{{"mount", "mount", "live_state"}, {"dtc_state", "dtc", "state"}, {"identity", "identity", "data_hex"}} {
				x, y := resolveMap(old[f.parent])[f.field], resolveMap(new[f.parent])[f.field]
				if !reflect.DeepEqual(x, y) {
					entry[f.key] = map[string]any{"before": x, "after": y}
				}
			}
			if d := snapshotDelta(snapshotDTCIndex(old), snapshotDTCIndex(new), "status", "status_changed"); d != nil {
				entry["dtcs"] = d
			}
			if d := snapshotDelta(snapshotFFDIndex(old), snapshotFFDIndex(new), "data_hex", "changed"); d != nil {
				entry["freeze_frames"] = d
			}
			oc, or := snapshotROBIndexes(old)
			nc, nr := snapshotROBIndexes(new)
			cd, rd := snapshotDelta(oc, nc, "", ""), snapshotDelta(or, nr, "data_hex", "changed")
			if cd != nil || rd != nil {
				if cd == nil {
					cd = map[string]any{"added": []map[string]any{}, "removed": []map[string]any{}}
				}
				if rd == nil {
					rd = map[string]any{"added": []map[string]any{}, "removed": []map[string]any{}, "changed": []map[string]any{}}
				}
				cd["records"] = rd
				entry["rob"] = cd
			}
		}
		if len(entry) > 5 {
			changes = append(changes, entry)
			for field, counter := range map[string]string{"mount": "mount_state_changes", "dtcs": "dtc_changes", "freeze_frames": "freeze_frame_changes", "rob": "rob_changes", "identity": "identity_changes"} {
				if entry[field] != nil {
					summary[counter] = snapshotInt(summary[counter]) + 1
				}
			}
		}
	}
	summary["changed_ecus"] = len(changes)
	return map[string]any{"schema": "toyota-health-check-diff-v1", "vehicle_type": after["vehicle_type"], "before_captured_at": before["captured_at"], "after_captured_at": after["captured_at"], "summary": summary, "changes": changes}, nil
}
func snapshotRequest(ctx context.Context, client RequestClient, request []byte) ([]byte, error) {
	if len(request) == 0 {
		return nil, fmt.Errorf("empty diagnostic request")
	}
	r, e := client.Request(ctx, request[0], request[1:])
	if e != nil {
		return nil, e
	}
	return append([]byte{request[0] + 0x40}, r...), nil
}
func snapshotErrorState(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "no_response"
	}
	var negative *transport.NegativeResponseError
	if errors.As(err, &negative) {
		return "negative_response"
	}
	return "error"
}
func snapshotCommand(catalog map[string]any, kind string) map[string]any {
	var result map[string]any
	for _, r := range resolveRows(catalog["commands"]) {
		if r["kind"] == kind {
			if result != nil {
				return nil
			}
			result = r
		}
	}
	return result
}
func GenericCIDPlan(catalog map[string]any) (map[string]any, int) {
	command := snapshotCommand(catalog, "generic_cid")
	rows := []map[string]any{}
	for _, r := range resolveRows(command["requests"]) {
		if r["resolved"] == true {
			rows = append(rows, r)
		}
	}
	if len(rows) != 1 {
		return nil, 0
	}
	request, e := resolveHex(rows[0]["send"])
	if e != nil || len(request) != 3 || request[0] != 0x22 {
		return nil, 0
	}
	check, e := resolveHex(rows[0]["check"])
	if e != nil || len(check) < 3 || !bytes.Equal(check[:3], append([]byte{0x62}, request[1:]...)) {
		return nil, 0
	}
	return command, int(binary.BigEndian.Uint16(request[1:]))
}
func P5SnapshotPlan(catalog map[string]any) map[string]any {
	command := snapshotCommand(catalog, "p5_dtc_snapshot")
	if command["execution"] != "read_only" || resolveMap(command["plugin_binding"])["dll"] != "GetEachFrzFrmDatP5_DT.dll" {
		return nil
	}
	rows := []map[string]any{}
	for _, r := range resolveRows(command["requests"]) {
		if r["resolved"] == true {
			rows = append(rows, r)
		}
	}
	if len(rows) != 1 {
		return nil
	}
	q, e := resolveHex(rows[0]["send"])
	if e != nil || !bytes.Equal(q, []byte{0x19, 4, 0, 0, 0, 255}) {
		return nil
	}
	check, e := resolveHex(rows[0]["check"])
	if e != nil || !bytes.Equal(check, []byte{0x59, 4}) {
		return nil
	}
	return command
}
func P5RoBPlan(catalog map[string]any) map[string]any {
	c := snapshotCommand(catalog, "p5_rob")
	binding := resolveMap(c["plugin_binding"])
	if c["execution"] != "read_only" || binding["dll"] != "GetRoBP5_DT.dll" || binding["exact_category_binding"] != true {
		return nil
	}
	protocols := resolveRows(c["protocols"])
	if len(protocols) != 2 {
		return nil
	}
	for i, p := range protocols {
		for j, phase := range []string{"inventory", "frames", "record"} {
			r := resolveMap(p[phase])
			if r["resolved"] != true {
				return nil
			}
			q, e := resolveHex(r["send"])
			if e != nil {
				return nil
			}
			want := []byte{0xab, byte(i*16 + j + 1)}
			want = append(want, make([]byte, j*2)...)
			if !bytes.Equal(q, want) {
				return nil
			}
			check, e := resolveHex(r["check"])
			if e != nil || !bytes.Equal(check, []byte{0xeb, want[1]}) {
				return nil
			}
		}
	}
	record := resolveMap(resolveMap(c["response_model"])["record"])
	if snapshotInt(record["payload_offset"]) != 6 || snapshotInt(record["block_count_offset"]) != 6 {
		return nil
	}
	return c
}
func SnapshotIdentity(ctx context.Context, client RequestClient, did int) map[string]any {
	r, e := ReadDID(ctx, client, uint16(did))
	out := map[string]any{"did": did, "state": "positive", "data_hex": nil, "ascii": nil}
	if e != nil {
		out["state"] = snapshotErrorState(e)
		out["error"] = e.Error()
		return out
	}
	out["data_hex"] = hex.EncodeToString(r)
	ascii := strings.TrimSpace(strings.TrimRight(string(r), "\x00"))
	valid := ascii != ""
	for _, c := range ascii {
		if c < 32 || c >= 127 {
			valid = false
		}
	}
	if valid {
		out["ascii"] = ascii
	}
	return out
}
func snapshotSignals(metadata map[string]any, key string) map[int][]map[string]any {
	out := map[int][]map[string]any{}
	for _, r := range resolveRows(metadata["signals"]) {
		if r[key] != nil {
			id := snapshotInt(r[key])
			out[id] = append(out[id], r)
		}
	}
	for _, rows := range out {
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := snapshotInt(rows[i]["sort_key"]), snapshotInt(rows[j]["sort_key"])
			if a != b {
				return a < b
			}
			secondary := "record_key"
			if key == "snapshot_did" {
				secondary = "monitor_key"
			}
			return snapshotInt(rows[i][secondary]) < snapshotInt(rows[j][secondary])
		})
	}
	return out
}
func DecodeSnapshotFFDRecord(record map[string]any, signals map[int][]map[string]any) (int, int, int) {
	payloads := map[int][]byte{}
	for _, id := range resolveRows(record["identifiers"]) {
		if id["did"] != nil {
			b, e := resolveHex(id["data_hex"])
			if e == nil {
				payloads[snapshotInt(id["did"])] = b
			}
		}
	}
	decodedCount, suppressedCount, errorCount := 0, 0, 0
	for _, id := range resolveRows(record["identifiers"]) {
		did := snapshotInt(id["did"])
		payload, e := resolveHex(id["data_hex"])
		decoded := []map[string]any{}
		errs := []string{}
		suppressed := 0
		if e != nil {
			errs = append(errs, e.Error())
		} else {
			for _, r := range signals[did] {
				value, e := DecodeFFDSignal(payload, r, payloads)
				if e != nil {
					errs = append(errs, fmt.Sprintf("%v: %v", r["name"], e))
					continue
				}
				if value["state"] == "not_supported" {
					suppressed++
					continue
				}
				for _, key := range []string{"bit_start", "bit_end", "primary_did"} {
					value[key] = snapshotInt(r[key])
				}
				decoded = append(decoded, value)
			}
		}
		state := "decoded"
		if len(signals[did]) == 0 {
			state = "no_schema"
		}
		if len(errs) > 0 {
			state = "decode_error"
			if len(decoded) > 0 {
				state = "partial"
			}
		}
		id["signal_decode"] = state
		id["signals"] = decoded
		id["suppressed_signal_count"] = suppressed
		id["decode_errors"] = errs
		decodedCount += len(decoded)
		suppressedCount += suppressed
		errorCount += len(errs)
	}
	return decodedCount, suppressedCount, errorCount
}
func SnapshotFreezeFrames(ctx context.Context, client RequestClient, command map[string]any, dtcs []map[string]any, metadata map[string]any) map[string]any {
	out := map[string]any{"state": "not_available", "dtcs": []map[string]any{}, "positive_dtc_count": 0, "record_count": 0, "decoded_signal_count": 0, "suppressed_signal_count": 0, "decode_error_count": 0}
	if command == nil {
		return out
	}
	out["state"] = "available"
	out["signal_decode"] = "raw_only"
	signals := snapshotSignals(metadata, "snapshot_did")
	if len(signals) > 0 {
		out["signal_decode"] = "gts_current_p5"
	}
	results := []map[string]any{}
	for _, d := range dtcs {
		code := resolveText(d["code"])
		row := map[string]any{"dtc": code, "state": "positive", "records": []map[string]any{}}
		number, e := DTCStringToNumber(code)
		if e != nil {
			row["state"] = "parse_error"
			row["error"] = e.Error()
			results = append(results, row)
			continue
		}
		response, e := client.Request(ctx, 0x19, []byte{4, byte(number >> 16), byte(number >> 8), byte(number), 255})
		if e != nil {
			row["state"] = snapshotErrorState(e)
			row["error"] = e.Error()
			results = append(results, row)
			continue
		}
		if len(response) == 0 || response[0] != 4 {
			row["state"] = "parse_error"
			row["error"] = "DTC snapshot subfunction echo mismatch"
			results = append(results, row)
			continue
		}
		parsed, e := ParseP5DTCSnapshotResponse(response[1:], number)
		if e != nil {
			row["state"] = "parse_error"
			row["error"] = e.Error()
			results = append(results, row)
			continue
		}
		records := resolveRows(parsed["records"])
		for _, rec := range records {
			a, b, c := DecodeSnapshotFFDRecord(rec, signals)
			out["decoded_signal_count"] = snapshotInt(out["decoded_signal_count"]) + a
			out["suppressed_signal_count"] = snapshotInt(out["suppressed_signal_count"]) + b
			out["decode_error_count"] = snapshotInt(out["decode_error_count"]) + c
		}
		row["status"] = parsed["status"]
		row["records"] = records
		out["positive_dtc_count"] = snapshotInt(out["positive_dtc_count"]) + 1
		out["record_count"] = snapshotInt(out["record_count"]) + len(records)
		results = append(results, row)
	}
	out["dtcs"] = results
	return out
}
func RenderSnapshot(document map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Health Check · %v\n", document["vehicle"])
	summary := resolveMap(document["summary"])
	fmt.Fprintf(&b, "%v candidates · %v responding · %v faults\n", summary["install_candidates"], summary["mount_responding"], summary["fault_status_records"])
	for _, row := range resolveRows(document["ecus"]) {
		dtc := resolveMap(row["dtc"])
		fmt.Fprintf(&b, "%s 0x%X  mount=%v dtc=%v\n", row["name"], snapshotInt(row["address"]), resolveMap(row["mount"])["live_state"], dtc["state"])
		for _, fault := range resolveRows(dtc["records"]) {
			if fault["fault_status"] == true {
				fmt.Fprintf(&b, "  %v status=0x%02X\n", fault["code"], snapshotInt(fault["status"]))
			}
		}
	}
	return strings.TrimSpace(b.String())
}
func RenderSnapshotDiff(document map[string]any) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Health Check changes · %v ECUs changed\n", resolveMap(document["summary"])["changed_ecus"])
	for _, r := range resolveRows(document["changes"]) {
		fmt.Fprintf(&b, "%v 0x%X\n", r["name"], snapshotInt(r["address"]))
		for _, key := range []string{"change", "mount", "dtc_state", "identity", "dtcs", "freeze_frames", "rob"} {
			if v := r[key]; v != nil {
				data, _ := json.Marshal(v)
				fmt.Fprintf(&b, "  %s: %s\n", key, data)
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// NewSnapshot preserves the wire-compatible snapshot envelope; acquisition adds ECU rows.
func NewSnapshot(profile, vehicle, region string, vehicleType, bus int, transport map[string]any, ecus []map[string]any, mounts []map[string]any) map[string]any {
	summary := map[string]any{"install_candidates": len(mounts), "mount_responding": 0, "mount_no_response": 0, "mount_probe_unavailable": 0, "dtc_positive_ecus": 0, "identity_positive_ecus": 0, "fault_status_records": 0}
	for _, r := range mounts {
		if r["transport_responded"] == true {
			summary["mount_responding"] = snapshotInt(summary["mount_responding"]) + 1
		}
		for _, state := range []string{"no_response", "probe_unavailable"} {
			if r["live_state"] == state {
				key := "mount_" + state
				summary[key] = snapshotInt(summary[key]) + 1
			}
		}
	}
	for _, r := range ecus {
		d := resolveMap(r["dtc"])
		if d["state"] == "positive" {
			summary["dtc_positive_ecus"] = snapshotInt(summary["dtc_positive_ecus"]) + 1
		}
		if resolveMap(r["identity"])["state"] == "positive" {
			summary["identity_positive_ecus"] = snapshotInt(summary["identity_positive_ecus"]) + 1
		}
		summary["fault_status_records"] = snapshotInt(summary["fault_status_records"]) + snapshotInt(d["fault_count"])
		for prefix, field := range map[string]string{"freeze_frame": "freeze_frames", "rob": "rob"} {
			part := resolveMap(r[field])
			key := prefix + "_available_ecus"
			if _, ok := summary[key]; !ok {
				summary[key] = 0
			}
			if part["state"] == "available" {
				summary[key] = snapshotInt(summary[key]) + 1
			}
			for source, suffix := range map[string]string{"positive_dtc_count": "positive_dtcs", "record_count": "records", "decoded_signal_count": "decoded_signals", "suppressed_signal_count": "suppressed_signals", "decode_error_count": "decode_errors", "behavior_code_count": "behavior_codes", "frame_count": "frames", "did_block_count": "did_blocks"} {
				if prefix == "freeze_frame" && (source == "behavior_code_count" || source == "frame_count" || source == "did_block_count") {
					continue
				}
				key = prefix + "_" + suffix
				summary[key] = snapshotInt(summary[key]) + snapshotInt(part[source])
			}
		}
	}
	return map[string]any{"schema": SnapshotSchema, "captured_at": time.Now().UTC().Format(time.RFC3339Nano), "profile": profile, "vehicle": vehicle, "vehicle_type": vehicleType, "region": region, "logical_bus": bus, "transport": transport, "summary": summary, "ecus": ecus, "coverage": map[string]any{"mount_support": "recovered family-local support modes", "dtc": "UDS ReadDTCInformation", "identity": "exact exported generic_cid request where available", "generic_ffd": "current-P5 per-DTC snapshots and exported FFD semantics", "operation_history": "current-P5 RoB behavior/frame/record transport and exported semantics", "info_code": "not_implemented", "monitor_data": "not_implemented", "timestamp_data": "not_implemented"}}
}
