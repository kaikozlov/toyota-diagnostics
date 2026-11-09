package diagnostic

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type SupportCheck func(context.Context, int) (bool, error)
type CustomizeValue struct {
	Payload []byte
	Value   uint64
	Choice  string
}
type CustomizeResult struct {
	Before, After CustomizeValue
	Written       []byte
	Changed       bool
}

func ResolveCustomizeChoice(item map[string]any, value string) (uint64, string, error) {
	choices, _ := item["choices"].([]any)
	numeric, e := strconv.ParseUint(value, 0, 64)
	if e == nil {
		var names []string
		for _, v := range choices {
			m := diagMap(v)
			if diagNum(m["value"]) == int(numeric) {
				names = append(names, diagString(m["name"]))
			}
		}
		if len(choices) > 0 && len(names) == 0 {
			return 0, "", fmt.Errorf("%s is not an OEM choice", value)
		}
		name := ""
		if len(names) == 1 {
			name = names[0]
		}
		return numeric, name, nil
	}
	var exact, partial []map[string]any
	for _, v := range choices {
		m := diagMap(v)
		name := strings.ToLower(diagString(m["name"]))
		needle := strings.ToLower(value)
		if name == needle {
			exact = append(exact, m)
		} else if strings.Contains(name, needle) {
			partial = append(partial, m)
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = partial
	}
	if len(matches) != 1 {
		return 0, "", fmt.Errorf("OEM choice %q resolves %d matches", value, len(matches))
	}
	n, e := diagInt(matches[0]["value"])
	if e != nil || n < 0 {
		return 0, "", fmt.Errorf("invalid OEM choice value")
	}
	return uint64(n), diagString(matches[0]["name"]), nil
}
func ExtractCustomizeValue(payload []byte, item map[string]any) (uint64, error) {
	a, e := diagInt(item["current_bit_start"])
	if e != nil {
		return 0, e
	}
	b, e := diagInt(item["current_bit_end"])
	if e != nil {
		return 0, e
	}
	if a < 0 || b < a || b >= len(payload)*8 || b-a >= 64 {
		return 0, fmt.Errorf("invalid Customize current bit geometry")
	}
	var v uint64
	for bit := a; bit <= b; bit++ {
		v = v<<1 | uint64((payload[bit/8]>>uint(7-bit%8))&1)
	}
	return v, nil
}
func MergeCustomizeValue(payload []byte, item map[string]any, value uint64) ([]byte, error) {
	a, e := diagInt(item["write_bit_start"])
	if e != nil {
		return nil, e
	}
	b, e := diagInt(item["write_bit_end"])
	if e != nil {
		return nil, e
	}
	mode, e := diagInt(item["merge_mode"])
	if e != nil {
		return nil, e
	}
	if mode != 0 && mode != 1 {
		return nil, fmt.Errorf("Customize merge mode %d is not supported", mode)
	}
	if a < 0 || b < a || b >= len(payload)*8 || b-a >= 64 {
		return nil, fmt.Errorf("invalid Customize write geometry")
	}
	width := b - a + 1
	if width < 64 && value >= uint64(1)<<width {
		return nil, fmt.Errorf("Customize value does not fit %d bits", width)
	}
	if mode == 1 {
		if width != 1 {
			return nil, fmt.Errorf("mode 1 requires one-bit field")
		}
		support := b/8 - 1
		mask := byte(0x80 >> uint(b%8))
		if support < 0 || payload[support]&mask != mask {
			return nil, fmt.Errorf("Customize field is not supported in current ECU buffer")
		}
	}
	out := bytes.Clone(payload)
	for i := 0; i < width; i++ {
		bit := b - i
		mask := byte(1 << uint(7-bit%8))
		out[bit/8] &= ^mask
		if value&(uint64(1)<<i) != 0 {
			out[bit/8] |= mask
		}
	}
	return out, nil
}
func customizeDecoded(payload []byte, item map[string]any) (CustomizeValue, error) {
	v, e := ExtractCustomizeValue(payload, item)
	if e != nil {
		return CustomizeValue{}, e
	}
	result := CustomizeValue{Payload: payload, Value: v}
	rows, _ := item["choices"].([]any)
	for _, r := range rows {
		m := diagMap(r)
		if diagNum(m["value"]) == int(v) {
			result.Choice = diagString(m["name"])
			break
		}
	}
	return result, nil
}
func ReadCustomize(ctx context.Context, c RequestClient, item map[string]any, support SupportCheck) (CustomizeValue, error) {
	did, e := diagInt(item["write_did"])
	if e != nil || did <= 0 || did > 65535 {
		return CustomizeValue{}, fmt.Errorf("invalid Customize DID")
	}
	if support == nil {
		return CustomizeValue{}, fmt.Errorf("Customize requires Toyota support inventory")
	}
	ok, e := support(ctx, did)
	if e != nil {
		return CustomizeValue{}, e
	}
	if !ok {
		return CustomizeValue{}, fmt.Errorf("Toyota inventory does not advertise Customize DID %04X", did)
	}
	payload, e := ReadDID(ctx, c, uint16(did))
	if e != nil {
		return CustomizeValue{}, e
	}
	return customizeDecoded(payload, item)
}
func SetCustomize(ctx context.Context, c RequestClient, item map[string]any, value uint64, support SupportCheck) (CustomizeResult, error) {
	before, e := ReadCustomize(ctx, c, item, support)
	if e != nil {
		return CustomizeResult{}, e
	}
	merged, e := MergeCustomizeValue(before.Payload, item, value)
	if e != nil {
		return CustomizeResult{}, e
	}
	result := CustomizeResult{Before: before, After: before, Written: merged}
	if bytes.Equal(merged, before.Payload) {
		return result, nil
	}
	did := uint16(diagNum(item["write_did"]))
	if e := WriteDID(ctx, c, did, merged); e != nil {
		return result, e
	}
	payload, e := ReadDID(ctx, c, did)
	if e != nil {
		return result, e
	}
	after, e := customizeDecoded(payload, item)
	if e != nil {
		return result, e
	}
	if !bytes.Equal(payload, merged) || after.Value != value {
		return result, fmt.Errorf("Customize post-write verification failed: expected %x value %d, got %x value %d", merged, value, payload, after.Value)
	}
	result.After = after
	result.Changed = true
	return result, nil
}

func (v CustomizeValue) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"payload_hex": hex.EncodeToString(v.Payload), "value": v.Value, "choice": v.Choice})
}
func (v CustomizeResult) MarshalJSON() ([]byte, error) {
	return json.Marshal(map[string]any{"before": v.Before, "after": v.After, "written_hex": hex.EncodeToString(v.Written), "changed": v.Changed})
}
