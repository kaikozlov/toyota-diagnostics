// Package diagnostic implements Toyota diagnostic interpretation independently of transport.
package diagnostic

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strconv"
	"strings"
)

const P5LinearMSB0V1 = "p5-linear-msb0-v1"

// ExtractMSB0 extracts an inclusive field, with bit zero at the first byte's MSB.
func ExtractMSB0(payload []byte, start, end int) (*big.Int, error) {
	if start < 0 || end < start {
		return nil, fmt.Errorf("invalid bit range %d..%d", start, end)
	}
	if end >= len(payload)*8 {
		return nil, fmt.Errorf("bits %d..%d exceed %d-byte DID payload", start, end, len(payload))
	}
	n := new(big.Int).SetBytes(payload[start/8 : end/8+1])
	n.Rsh(n, uint(7-end%8))
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(end-start+1)), big.NewInt(1))
	return n.And(n, mask), nil
}

// ConvertP5Physical follows the native integer conversion: division truncates toward zero.
func ConvertP5Physical(raw *big.Int, width int, signed bool, mul, div, offset int64) (*big.Int, error) {
	if width <= 0 {
		return nil, fmt.Errorf("invalid bit width %d", width)
	}
	modulus := new(big.Int).Lsh(big.NewInt(1), uint(width))
	n := new(big.Int).And(raw, new(big.Int).Sub(new(big.Int).Set(modulus), big.NewInt(1)))
	if signed && n.Bit(width-1) != 0 {
		n.Sub(n, modulus)
	}
	n.Mul(n, big.NewInt(mul))
	if div > 1 {
		n.Quo(n, big.NewInt(div))
	}
	return n.Add(n, big.NewInt(offset)), nil
}

func FormatP5Decimal(n *big.Int, decimals int) (string, error) {
	if decimals < 0 {
		return "", fmt.Errorf("invalid decimal point count %d", decimals)
	}
	s := new(big.Int).Abs(n).String()
	if decimals > 0 {
		if len(s) <= decimals {
			s = strings.Repeat("0", decimals+1-len(s)) + s
		}
		s = s[:len(s)-decimals] + "." + s[len(s)-decimals:]
	}
	if n.Sign() < 0 {
		s = "-" + s
	}
	return s, nil
}

func decodeInt(row map[string]any, key string) (int64, error) {
	v, ok := row[key]
	if !ok || v == nil {
		return 0, fmt.Errorf("missing %s", key)
	}
	switch n := v.(type) {
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case uint64:
		if n <= uint64(^uint64(0)>>1) {
			return int64(n), nil
		}
	case float64:
		if n >= -9223372036854775808 && n < 9223372036854775808 && n == float64(int64(n)) {
			return int64(n), nil
		}
	case json.Number:
		return n.Int64()
	case string:
		return strconv.ParseInt(n, 10, 64)
	}
	return 0, fmt.Errorf("invalid %s: %v", key, v)
}
func decodeDefaultInt(row map[string]any, key string) int64 { n, _ := decodeInt(row, key); return n }
func decodeBool(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	case int64:
		return x != 0
	case float64:
		return x != 0
	case string:
		return x != ""
	}
	return false
}
func decodeNumber(n *big.Int) any {
	if n.IsInt64() {
		return n.Int64()
	}
	return json.Number(n.String())
}

func DecodeSignal(payload []byte, row map[string]any) (map[string]any, error) {
	if row["decoder"] != P5LinearMSB0V1 {
		return nil, fmt.Errorf("unsupported decoder %v", row["decoder"])
	}
	keys := []string{"bit_start", "bit_end", "mul", "div", "offset", "decimal_point_count"}
	ns := make([]int64, len(keys))
	for i, k := range keys {
		n, e := decodeInt(row, k)
		if e != nil {
			return nil, fmt.Errorf("incomplete %s metadata: %w", P5LinearMSB0V1, e)
		}
		ns[i] = n
	}
	raw, e := ExtractMSB0(payload, int(ns[0]), int(ns[1]))
	if e != nil {
		return nil, e
	}
	n, e := ConvertP5Physical(raw, int(ns[1]-ns[0]+1), decodeBool(row["signed"]), ns[2], ns[3], ns[4])
	if e != nil {
		return nil, e
	}
	value, e := FormatP5Decimal(n, int(ns[5]))
	if e != nil {
		return nil, e
	}
	var pattern any
	switch p := row["patterns"].(type) {
	case map[string]any:
		pattern = p[n.String()]
	case map[string]string:
		if s, ok := p[n.String()]; ok {
			pattern = s
		}
	}
	return map[string]any{"raw": decodeNumber(raw), "converted_integer": decodeNumber(n), "value": value, "pattern": pattern}, nil
}

func FormatDecodedSignal(payload []byte, row map[string]any) (string, error) {
	result, e := DecodeSignal(payload, row)
	if e != nil {
		return "", e
	}
	raw, _ := ExtractMSB0(payload, int(decodeDefaultInt(row, "bit_start")), int(decodeDefaultInt(row, "bit_end")))
	width := int(decodeDefaultInt(row, "bit_end") - decodeDefaultInt(row, "bit_start") + 1)
	rendered := fmt.Sprint(result["value"])
	if result["pattern"] != nil {
		rendered = fmt.Sprint(result["pattern"])
	} else if u := row["unit"]; u != nil && u != "" {
		rendered += " " + fmt.Sprint(u)
	}
	name := row["name"]
	if name == nil || name == "" {
		name = "(unnamed)"
	}
	return fmt.Sprintf("%v: %s (raw=0x%0*X)", name, rendered, (width+3)/4, raw), nil
}

func P5LocalSupported(payload []byte, row map[string]any) (bool, error) {
	end, e := decodeInt(row, "bit_end")
	if e != nil {
		return false, fmt.Errorf("incomplete current-P5 RoB support metadata")
	}
	mode, e := decodeInt(row, "local_support_mode")
	if e != nil {
		return false, fmt.Errorf("incomplete current-P5 RoB support metadata")
	}
	if end >= int64(len(payload)*8) {
		return false, nil
	}
	switch mode {
	case 0, 2:
		return true, nil
	case 1:
		i := (end >> 3) - 1
		if i < 0 || i >= int64(len(payload)) {
			return false, nil
		}
		mask := byte(0x80 >> uint(end&7))
		return payload[i]&mask == mask, nil
	}
	return false, fmt.Errorf("unsupported current-P5 RoB local support mode %d", mode)
}
func decodeExportedP5(payload []byte, row map[string]any) (map[string]any, error) {
	info, ok := row["signal_info"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("signal has no exported physical metadata")
	}
	r := map[string]any{"decoder": P5LinearMSB0V1, "name": row["name"], "bit_start": row["bit_start"], "bit_end": row["bit_end"]}
	for _, key := range []string{"mul", "div", "offset", "signed", "decimal_point_count", "unit"} {
		r[key] = info[key]
	}
	r["patterns"] = info["pattern_display"]
	d, e := DecodeSignal(payload, r)
	if e != nil {
		return nil, e
	}
	rendered := fmt.Sprint(d["value"])
	if d["pattern"] != nil {
		rendered = fmt.Sprint(d["pattern"])
	} else if u := info["unit"]; u != nil && u != "" {
		rendered += " " + fmt.Sprint(u)
	}
	d["state"] = "decoded"
	d["name"] = row["name"]
	d["unit"] = info["unit"]
	d["formatted"] = rendered
	return d, nil
}
func DecodeFFDSignal(payload []byte, row map[string]any, recordPayloads map[int][]byte) (map[string]any, error) {
	if decodeBool(row["dynamic_lsb_possible"]) {
		return nil, fmt.Errorf("FFD dynamic-LSB materialization is required before decoding this signal")
	}
	absent := map[string]any{"state": "not_supported", "name": row["name"]}
	supported, e := P5LocalSupported(payload, row)
	if e != nil {
		return nil, e
	}
	if !supported {
		return absent, nil
	}
	if c := row["support_condition"]; c != nil {
		condition, ok := c.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid FFD support condition metadata")
		}
		vals := make([]int64, 4)
		for i, k := range []string{"condition_type", "referenced_did", "bit_start", "bit_end"} {
			n, e := decodeInt(condition, k)
			if e != nil {
				return nil, fmt.Errorf("incomplete FFD support condition metadata")
			}
			vals[i] = n
		}
		if vals[0] != 7 {
			return nil, fmt.Errorf("unsupported FFD support condition type %d", vals[0])
		}
		p, ok := recordPayloads[int(vals[1])]
		if !ok {
			return absent, nil
		}
		n, e := ExtractMSB0(p, int(vals[2]), int(vals[3]))
		if e != nil {
			return nil, e
		}
		if n.Sign() == 0 {
			return absent, nil
		}
	}
	d, e := decodeExportedP5(payload, row)
	if e != nil {
		return nil, e
	}
	d["monitor_key"] = decodeDefaultInt(row, "monitor_key")
	d["sort_key"] = decodeDefaultInt(row, "sort_key")
	return d, nil
}
func DecodeRoBSignal(payload []byte, row map[string]any) (map[string]any, error) {
	if decodeDefaultInt(row, "support_condition_key") != 0 {
		return nil, fmt.Errorf("RoB cross-DID support condition metadata is required before decoding this signal")
	}
	if decodeBool(row["dynamic_lsb_possible"]) {
		return nil, fmt.Errorf("RoB dynamic-LSB materialization is required before decoding this signal")
	}
	if decodeDefaultInt(row, "extraction_mode") == 4 {
		return nil, fmt.Errorf("RoB extraction mode 4 is an opaque buffer field, not an integer signal")
	}
	supported, e := P5LocalSupported(payload, row)
	if e != nil {
		return nil, e
	}
	if !supported {
		return map[string]any{"state": "not_supported", "name": row["name"]}, nil
	}
	d, e := decodeExportedP5(payload, row)
	if e != nil {
		return nil, e
	}
	d["record_key"] = decodeDefaultInt(row, "record_key")
	d["sort_key"] = decodeDefaultInt(row, "sort_key")
	return d, nil
}
