package diagnostic

import (
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

func unhex(s string) []byte {
	b, e := hex.DecodeString(s)
	if e != nil {
		panic(e)
	}
	return b
}
func TestDTCCode(t *testing.T) {
	for _, code := range []string{"P000121", "C123456", "B123456", "U013187", "U3FFFFF"} {
		n, e := DTCStringToNumber(code)
		if e != nil || DTCNumberToString(n) != code {
			t.Fatal(code, n, e)
		}
	}
	for _, code := range []string{"X013187", "P400000", "P0012", "P00zzzz"} {
		if _, e := DTCStringToNumber(code); e == nil {
			t.Fatal(code)
		}
	}
	got, e := ParseDTCResponse(unhex("ff00012108"))
	if e != nil || !reflect.DeepEqual(got, []DTCRecord{{"P000121", 8}}) {
		t.Fatal(got, e)
	}
	if got, e := ParseDTCResponse(nil); e != nil || len(got) != 0 {
		t.Fatal(got, e)
	}
	if _, e := ParseDTCResponse([]byte{255, 0}); e == nil {
		t.Fatal("malformed accepted")
	}
}
func TestP5Snapshot(t *testing.T) {
	data := unhex("c13187080102123402aabb567801cc10019abc03010203")
	got, e := ParseP5DTCSnapshotResponse(data, 0xc13187)
	if e != nil {
		t.Fatal(e)
	}
	if got["dtc"] != "U013187" || got["status"] != 8 {
		t.Fatal(got)
	}
	records := got["records"].([]map[string]any)
	if len(records) != 2 || records[0]["record_number"] != 1 || records[1]["ffd_type"] != 0x10 {
		t.Fatal(records)
	}
	ids := records[0]["identifiers"].([]map[string]any)
	if len(ids) != 2 || ids[0]["data_hex"] != "aabb" || ids[1]["did"] != 0x5678 {
		t.Fatal(ids)
	}
	for _, tc := range []struct {
		data  string
		dtc   uint32
		error string
	}{
		{"c13187080101123404aabb", 0xc13187, "exceeds response"},
		{"c1318708", 0xc13188, "echo mismatch"},
		{"c131", 0xc13187, "length"},
		{"c131870801", 0xc13187, "record header"},
		{"c1318708010112", 0xc13187, "DID header"},
		{"c1318708000001", 0xc13187, "trailing"},
	} {
		if _, e := ParseP5DTCSnapshotResponse(unhex(tc.data), tc.dtc); e == nil || !strings.Contains(e.Error(), tc.error) {
			t.Fatal(tc, e)
		}
	}
	if _, e := ParseP5DTCSnapshotResponse(unhex("c131870800000000"), 0xc13187); e != nil {
		t.Fatal(e)
	}
	for _, tc := range [][2]int{{1, 1}, {2, 2}, {0x10, 0x10}, {0x2f, 0x10}, {0x30, 0x30}, {0x4f, 0x30}, {0x50, 0x50}, {0x6f, 0x50}, {0x70, 0}, {0, 0}} {
		if FFDType(byte(tc[0])) != tc[1] {
			t.Fatal(tc)
		}
	}
}
func TestDTCStatus(t *testing.T) {
	s, e := DecodeStatusBits(9)
	if e != nil || !reflect.DeepEqual(s, []string{"TEST_FAILED", "CONFIRMED_DTC"}) {
		t.Fatal(s, e)
	}
	if _, e := DecodeStatusBits(256); e == nil {
		t.Fatal("accepted")
	}
}
