package catalog

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestDTCSourceDisabledAndUnknown(t *testing.T) {
	v := []any{int64(0), int64(0), int64(4), int64(65535), int64(255), int64(47), int64(2), nil, nil, nil, []byte{0x12, 0xff}, "ABS.ddb", int64(5), int64(0), "P0001", nil, "", nil, int64(1), nil, int64(23), int64(7), nil, int64(2), nil, int64(1)}
	got, e := decodeDTCSourceValues(v)
	if e != nil {
		t.Fatal(e)
	}
	if got["enabled"] != false || got["exception_handler_id"] != json.Number("65535") || got["detail_lookup_key"] != json.Number("2") || got["uninterpreted_bytes"].(map[string]any)["24_25"] != "12ff" {
		t.Fatal(got)
	}
	for _, i := range []int{0, 1, 3, 4, 5, 6, 10, 11, 12, 18, 20, 21, 23} {
		bad := append([]any(nil), v...)
		bad[i] = nil
		if _, e = decodeDTCSourceValues(bad); e == nil {
			t.Fatalf("corrupt column %d accepted", i)
		}
	}
}
func TestDTCSourcePackedAndUnknown(t *testing.T) {
	v := []any{int64(2), int64(0), int64(0), int64(0), int64(0), nil, nil, int64(0), int64(255), int64(65535), []byte{0xff}, "ABS.ddb", int64(65), int64(0), "P1234-56", nil, nil, nil, nil, int64(0x123456), int64(47), int64(7), nil, nil, nil, int64(1)}
	got, e := decodeDTCSourceValues(v)
	if e != nil {
		t.Fatal(e)
	}
	if got["enabled"] != true || got["packed_dtc"] != json.Number("1193046") || got["base_dtc"] != json.Number("4660") || got["failure_type"] != json.Number("86") {
		t.Fatal(got)
	}
	info := got["diag_code_info_fields"].([]any)
	if info[0].(map[string]any)["value"] != json.Number("255") {
		t.Fatal(info)
	}
}
func TestDTCSourceCorpusProof(t *testing.T) {
	path := os.Getenv("TOYO_DTC_SOURCE_PROOF")
	if path == "" {
		t.Skip("optional independent corpus proof")
	}
	db, e := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	d := &DB{db: db}
	f, e := os.Open(os.Getenv("TOYO_DTC_SOURCE_EXPECTED"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	count := 0
	var nodes, expected []any
	for scanner.Scan() {
		var row struct {
			ID    int64
			Value map[string]any
		}
		dec := json.NewDecoder(strings.NewReader(scanner.Text()))
		dec.UseNumber()
		if e = dec.Decode(&row); e != nil {
			t.Fatal(e)
		}
		got, err := d.dtcSourceDefinition(context.Background(), row.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, row.Value) {
			t.Fatalf("source DTC%d mismatch\ngot%v\nwant%v", row.ID, got, row.Value)
		}
		nodes = append(nodes, map[string]any{"$dtc_source_definition": json.Number(strconv.FormatInt(row.ID, 10))})
		expected = append(expected, row.Value)
		count++
	}
	if e = scanner.Err(); e != nil {
		t.Fatal(e)
	}
	if count == 0 {
		t.Fatal("empty proof")
	}
	got, handled, err := d.dtcSourceArray(context.Background(), nodes, nil)
	if err != nil || !handled || !reflect.DeepEqual(got, expected) {
		t.Fatalf("batch corpus mismatch: %v", err)
	}
	duplicate := make([]any, 300)
	for i := range duplicate {
		duplicate[i] = nodes[0]
	}
	decoded, err := d.expandMetadata(context.Background(), duplicate, referenceStack{})
	if err != nil {
		t.Fatal(err)
	}
	list := decoded.([]any)
	list[0].(map[string]any)["uninterpreted_bytes"].(map[string]any)["changed"] = "x"
	if _, exists := list[1].(map[string]any)["uninterpreted_bytes"].(map[string]any)["changed"]; exists {
		t.Fatal("duplicate aliases")
	}
	duplicate[0] = map[string]any{"other": 1}
	if _, ok, err := d.dtcSourceArray(context.Background(), duplicate, nil); ok || err != nil {
		t.Fatal("mixed array intercepted")
	}
	duplicate[0] = map[string]any{"$dtc_source_definition": json.Number("999999999")}
	if _, _, err := d.dtcSourceArray(context.Background(), duplicate, nil); err == nil {
		t.Fatal("missing accepted")
	}
	t.Logf("exact point and batch source DTC definitions %d", count)
}
