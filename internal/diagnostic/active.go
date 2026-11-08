package diagnostic

import (
	"fmt"
	"math/big"
	"strings"
)

func directRange(row map[string]any, n int) (int, int, error) {
	a, e := diagInt(row["bit_start"])
	if e != nil {
		return 0, 0, e
	}
	b, e := diagInt(row["bit_end"])
	if e != nil {
		return 0, 0, e
	}
	if n <= 0 || a < 0 || b < a || b >= n*8 {
		return 0, 0, fmt.Errorf("direct bit range %d..%d does not fit %d bytes", a, b, n)
	}
	return a, b, nil
}
func PackDirectRawValue(row map[string]any, n int, value uint64) ([]byte, error) {
	a, b, e := directRange(row, n)
	if e != nil {
		return nil, e
	}
	mode, e := diagInt(row["encoding_mode"])
	if e != nil {
		return nil, e
	}
	width := b - a + 1
	if width < 64 && value >= uint64(1)<<width {
		return nil, fmt.Errorf("raw value does not fit %d bits", width)
	}
	switch mode {
	case 0, 3:
		if a%8 != 0 || b%8 != 7 || (b/8-a/8+1) > 4 {
			return nil, fmt.Errorf("mode %d requires 1..4 byte aligned field", mode)
		}
	case 1, 6:
		if a/8 != b/8 || width > 8 {
			return nil, fmt.Errorf("mode %d requires single byte field", mode)
		}
	case 4:
		if b/8-a/8+1 > 4 {
			return nil, fmt.Errorf("mode 4 requires 1..4 byte span")
		}
	default:
		return nil, fmt.Errorf("unrecovered direct encoding mode %d", mode)
	}
	out := make([]byte, n)
	for i := 0; i < width; i++ {
		if value&(uint64(1)<<i) != 0 {
			bit := b - i
			out[bit/8] |= 1 << uint(7-bit%8)
		}
	}
	return out, nil
}
func DirectControlEnableMasks(row map[string]any, n int) ([]byte, []byte, error) {
	a, b, e := directRange(row, n)
	if e != nil {
		return nil, nil, e
	}
	mode, e := diagInt(row["encoding_mode"])
	if e != nil {
		return nil, nil, e
	}
	strategy := diagMap(row["control_enable_mask"])
	if strategy == nil {
		return nil, nil, fmt.Errorf("missing recovered control enable strategy")
	}
	expected := map[int]string{0: "type67_rows_for_selected_byte_span", 1: "selected_bit_range_or_alternate", 3: "none", 4: "selected_bit_range", 6: "none"}
	want, ok := expected[mode]
	if !ok {
		return nil, nil, fmt.Errorf("unsupported mask encoding mode %d", mode)
	}
	legacy := (mode == 1 || mode == 4) && strategy["start"] == "none" && strategy["stop"] == "selected_bit_range"
	if !legacy && (strategy["start"] != want || strategy["stop"] != want) {
		return nil, nil, fmt.Errorf("mask strategy disagrees with recovered mode %d", mode)
	}
	mask := []byte{}
	switch mode {
	case 1, 4:
		mask = make([]byte, n)
		for bit := a; bit <= b; bit++ {
			mask[bit/8] |= 1 << uint(7-bit%8)
		}
	case 0:
		records, ok := row["data_id_for_act_records"].([]any)
		if !ok || len(records) == 0 {
			return nil, nil, fmt.Errorf("missing type67 mask geometry")
		}
		spanStart, spanEnd := a/8, a/8+(b-a)/8+1
		for _, v := range records {
			m := diagMap(v)
			bit, e := diagInt(m["control_enable_bit_1based"])
			if e != nil {
				return nil, nil, e
			}
			offset, e := diagInt(m["data_byte_offset"])
			if e != nil {
				return nil, nil, e
			}
			length, e := diagInt(m["data_byte_length"])
			if e != nil {
				return nil, nil, e
			}
			mode, e := diagInt(m["encoding_mode"])
			if e != nil || mode != 0 {
				return nil, nil, fmt.Errorf("invalid type67 encoding mode")
			}
			if offset >= spanStart && offset < spanEnd && offset+length <= spanEnd {
				if bit <= 0 {
					return nil, nil, fmt.Errorf("type67 mask bit must be positive")
				}
				bit--
				for len(mask) <= bit/8 {
					mask = append(mask, 0)
				}
				mask[bit/8] |= 1 << uint(7-bit%8)
			}
		}
	}
	return mask, append([]byte(nil), mask...), nil
}
func engineeringIntegerToRaw(row map[string]any, value *big.Int) (uint64, error) {
	p := diagMap(diagMap(row["signal_info"])["physical"])
	mul, e := diagInt(p["mul"])
	if e != nil || mul == 0 {
		return 0, fmt.Errorf("invalid physical multiplier")
	}
	div, e := diagInt(p["div"])
	if e != nil {
		return 0, e
	}
	offset, e := diagInt(p["offset"])
	if e != nil {
		return 0, e
	}
	n := new(big.Int).Sub(value, big.NewInt(int64(offset)))
	n.Mul(n, big.NewInt(int64(div)))
	n.Quo(n, big.NewInt(int64(mul)))
	n.And(n, big.NewInt(255))
	return n.Uint64(), nil
}
func DirectEngineeringToRaw(row map[string]any, value string) (uint64, error) {
	p := diagMap(diagMap(row["signal_info"])["physical"])
	digits, e := diagInt(p["decimal_point_count"])
	if e != nil || digits < 0 || digits > 1000 {
		return 0, fmt.Errorf("invalid decimal precision")
	}
	r, ok := new(big.Rat).SetString(value)
	if !ok {
		return 0, fmt.Errorf("invalid finite engineering value %q", value)
	}
	r.Mul(r, new(big.Rat).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(digits)), nil)))
	if !r.IsInt() {
		return 0, fmt.Errorf("engineering value exceeds recovered %d decimal precision", digits)
	}
	return engineeringIntegerToRaw(row, r.Num())
}
func DirectChoiceToRaw(row map[string]any, choice string) (uint64, error) {
	rows, _ := diagMap(row["signal_info"])["choices"].([]any)
	var matches []map[string]any
	for _, v := range rows {
		m := diagMap(v)
		if strings.EqualFold(diagString(m["text"]), choice) {
			matches = append(matches, m)
		}
	}
	if len(matches) != 1 {
		return 0, fmt.Errorf("OEM choice %q resolves %d matches", choice, len(matches))
	}
	n, e := diagInt(matches[0]["value"])
	if e != nil {
		return 0, e
	}
	return engineeringIntegerToRaw(row, big.NewInt(int64(n)))
}
func ResolveMultiDirectGroup(group map[string]any, lookup func(int) (map[string]any, error)) TestPlan {
	p := TestPlan{ID: diagNum(group["group_id"]), Name: diagString(group["name"]), Kind: "multi_direct", Row: group}
	if group["execution"] != "materializable" {
		p.Refusals = append(p.Refusals, "group is not materializable")
	}
	if group["composer"] != "or_member_start_stop_frames" {
		p.Refusals = append(p.Refusals, "unsupported group composer")
	}
	id, e := diagInt(group["did"])
	if e != nil || id < 0 || id >= 65535 {
		p.Refusals = append(p.Refusals, "group has no single recovered DID")
	} else {
		p.Identifier = uint16(id)
	}
	rows, ok := group["member_inputs"].([]any)
	if !ok || len(rows) == 0 {
		p.Refusals = append(p.Refusals, "missing member inputs")
	}
	ids, slots := map[int]bool{}, map[int]bool{}
	for _, v := range rows {
		m := diagMap(v)
		mid, e := diagInt(m["active_test_id"])
		if e != nil {
			p.Refusals = append(p.Refusals, "invalid member id")
			continue
		}
		slot, e := diagInt(m["input_slot"])
		if e != nil || slot < 1 || slot > 2 {
			p.Refusals = append(p.Refusals, "invalid member input slot")
		}
		if ids[mid] || slots[slot] {
			p.Refusals = append(p.Refusals, "duplicate member or input slot")
		}
		ids[mid] = true
		slots[slot] = true
		row, e := lookup(mid)
		if e != nil {
			p.Refusals = append(p.Refusals, e.Error())
			continue
		}
		member := ResolvePlan(row)
		if diagNum(m["did"]) != id || int(member.Identifier) != id {
			p.Refusals = append(p.Refusals, "member DID disagrees with group")
		}
		if !CanMaterializeDirect(member) {
			p.Refusals = append(p.Refusals, "member has no exact runtime length probe")
		}
		if p.SessionRequirement != "" && p.SessionRequirement != member.SessionRequirement {
			p.Refusals = append(p.Refusals, "members disagree on session requirement")
		}
		p.SessionRequirement = member.SessionRequirement
		p.Members = append(p.Members, member)
		p.InputSlots = append(p.InputSlots, slot)
	}
	if len(p.Refusals) == 0 {
		p.Refusals = append(p.Refusals, "runtime payload length not definitively recovered")
	}
	return p
}
func ComposeMultiDirectPayload(p TestPlan, values map[int]uint64) ([]byte, []byte, []byte, error) {
	if p.RuntimeLength <= 0 || len(p.Refusals) > 0 {
		return nil, nil, nil, p.Err()
	}
	if len(values) != len(p.Members) {
		return nil, nil, nil, fmt.Errorf("group values must exactly match members")
	}
	payload := make([]byte, p.RuntimeLength)
	var start, stop []byte
	or := func(out, in []byte) []byte {
		for len(out) < len(in) {
			out = append(out, 0)
		}
		for i, b := range in {
			out[i] |= b
		}
		return out
	}
	for _, m := range p.Members {
		v, ok := values[m.ID]
		if !ok {
			return nil, nil, nil, fmt.Errorf("missing value for member %04X", m.ID)
		}
		if m.Identifier != p.Identifier {
			return nil, nil, nil, fmt.Errorf("member DID mismatch")
		}
		b, e := PackDirectRawValue(m.Row, p.RuntimeLength, v)
		if e != nil {
			return nil, nil, nil, e
		}
		sm, em, e := DirectControlEnableMasks(m.Row, p.RuntimeLength)
		if e != nil {
			return nil, nil, nil, e
		}
		payload = or(payload, b)
		start = or(start, sm)
		stop = or(stop, em)
	}
	return payload, start, stop, nil
}
