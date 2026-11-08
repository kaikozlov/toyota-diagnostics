package diagnostic

import (
	"math/big"
	"strings"
	"testing"
)

func steeringRow() map[string]any {
	return map[string]any{"decoder": P5LinearMSB0V1, "name": "Steering Angle", "bit_start": 0, "bit_end": 15, "mul": 15, "div": 1, "offset": 0, "decimal_point_count": 1, "unit": "deg"}
}
func exportedRow() map[string]any {
	return map[string]any{"name": "Steering Angle", "bit_start": 0, "bit_end": 15, "local_support_mode": 0, "monitor_key": 17, "sort_key": 17, "signal_info": map[string]any{"mul": 15, "div": 1, "offset": 0, "decimal_point_count": 1, "unit": "deg"}}
}
func TestExtractMSB0(t *testing.T) {
	n, e := ExtractMSB0([]byte{0xa5, 0x3c}, 4, 11)
	if e != nil || n.Uint64() != 0x53 {
		t.Fatal(n, e)
	}
	n, e = ExtractMSB0([]byte{255, 255, 255, 255, 255, 255, 255, 255, 255}, 0, 71)
	if e != nil || n.String() != "4722366482869645213695" {
		t.Fatal(n, e)
	}
	for _, r := range [][2]int{{-1, 0}, {2, 1}, {0, 16}} {
		if _, e := ExtractMSB0([]byte{1, 2}, r[0], r[1]); e == nil {
			t.Fatal(r)
		}
	}
}
func TestPhysicalAndDecimal(t *testing.T) {
	n, e := ConvertP5Physical(big.NewInt(255), 8, true, 5, 2, 0)
	if e != nil || n.Int64() != -2 {
		t.Fatal(n, e)
	}
	for _, div := range []int64{-1, 0, 1} {
		n, e := ConvertP5Physical(big.NewInt(3), 8, false, 2, div, 1)
		if e != nil || n.Int64() != 7 {
			t.Fatal(n, e)
		}
	}
	for _, x := range []struct {
		n      int64
		places int
		want   string
	}{{-15, 1, "-1.5"}, {15, 3, "0.015"}, {0, 2, "0.00"}, {-1, 0, "-1"}} {
		s, e := FormatP5Decimal(big.NewInt(x.n), x.places)
		if e != nil || s != x.want {
			t.Fatal(s, e)
		}
	}
	if _, e := FormatP5Decimal(big.NewInt(1), -1); e == nil {
		t.Fatal("negative decimal count accepted")
	}
}
func TestDecodeSignal(t *testing.T) {
	row := steeringRow()
	got, e := DecodeSignal([]byte{0, 1}, row)
	if e != nil || got["raw"] != int64(1) || got["converted_integer"] != int64(15) || got["value"] != "1.5" || got["pattern"] != nil {
		t.Fatal(got, e)
	}
	s, e := FormatDecodedSignal([]byte{0, 1}, row)
	if e != nil || s != "Steering Angle: 1.5 deg (raw=0x0001)" {
		t.Fatal(s, e)
	}
	row["patterns"] = map[string]any{"15": "ON"}
	s, e = FormatDecodedSignal([]byte{0, 1}, row)
	if e != nil || s != "Steering Angle: ON (raw=0x0001)" {
		t.Fatal(s, e)
	}
	for _, decoder := range []any{nil, "unknown", "p6-native-msb0-u32-v1"} {
		if _, e := DecodeSignal([]byte{0}, map[string]any{"decoder": decoder}); e == nil || !strings.Contains(e.Error(), "unsupported decoder") {
			t.Fatal(e)
		}
	}
	if _, e := DecodeSignal([]byte{0}, steeringRow()); e == nil || !strings.Contains(e.Error(), "exceed 1-byte") {
		t.Fatal(e)
	}
	delete(row, "mul")
	if _, e := DecodeSignal([]byte{0, 1}, row); e == nil {
		t.Fatal("missing metadata accepted")
	}
}
func TestFFDAndRoB(t *testing.T) {
	row := exportedRow()
	got, e := DecodeFFDSignal([]byte{0, 1}, row, map[int][]byte{})
	if e != nil || got["formatted"] != "1.5 deg" || got["monitor_key"] != int64(17) {
		t.Fatal(got, e)
	}
	row["support_condition"] = map[string]any{"condition_type": 7, "referenced_did": 0x403, "bit_start": 7, "bit_end": 7}
	for _, payload := range [][]byte{nil, {0}, {1}} {
		refs := map[int][]byte{}
		if payload != nil {
			refs[0x403] = payload
		}
		got, e = DecodeFFDSignal([]byte{0, 1}, row, refs)
		if e != nil {
			t.Fatal(e)
		}
		want := "not_supported"
		if payload != nil && payload[0] == 1 {
			want = "decoded"
		}
		if got["state"] != want {
			t.Fatal(got)
		}
	}
	delete(row, "support_condition")
	row["record_key"] = 7
	got, e = DecodeRoBSignal([]byte{0, 1}, row)
	if e != nil || got["record_key"] != int64(7) || got["formatted"] != "1.5 deg" {
		t.Fatal(got, e)
	}
	row["local_support_mode"] = 1
	for _, p := range [][]byte{{1, 1}, {0, 1}} {
		got, e = DecodeRoBSignal(p, row)
		if e != nil {
			t.Fatal(e)
		}
		want := "not_supported"
		if p[0] == 1 {
			want = "decoded"
		}
		if got["state"] != want {
			t.Fatal(got)
		}
	}
	for _, field := range []string{"support_condition_key", "dynamic_lsb_possible", "extraction_mode"} {
		r := exportedRow()
		r[field] = 1
		if field == "dynamic_lsb_possible" {
			r[field] = true
		}
		if field == "extraction_mode" {
			r[field] = 4
		}
		if _, e := DecodeRoBSignal([]byte{0, 1}, r); e == nil {
			t.Fatal(field)
		}
	}
}
