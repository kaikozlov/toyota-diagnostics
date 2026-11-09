package catalog

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"reflect"
	"testing"
)

func TestROBUnknownBytes(t *testing.T) {
	for _, test := range []struct {
		table int64
		blob  []byte
		want  map[string]any
	}{{163, []byte{1, 0xff, 3}, map[string]any{"65_67": "01ff03"}}, {164, []byte{1, 2, 3, 4, 5, 6, 7}, map[string]any{"38_39": "0102", "43_47": "0304050607"}}} {
		out := map[string]any{}
		if e := restoreROBUnknown(out, test.blob, test.table); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(out["uninterpreted_bytes"], test.want) {
			t.Fatal(out)
		}
		if e := restoreROBUnknown(out, []byte{}, test.table); e == nil {
			t.Fatal("bad width accepted")
		}
		absent := map[string]any{}
		if e := restoreROBUnknown(absent, nil, test.table); e != nil || len(absent) != 0 {
			t.Fatal(absent, e)
		}
	}
}
func TestROBUnknownCorpus(t *testing.T) {
	path := os.Getenv("TOYO_ROB_UNKNOWN_PROOF")
	if path == "" {
		t.Skip("optional independent source proof")
	}
	db, e := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	d := &DB{db: db}
	f, e := os.Open(os.Getenv("TOYO_ROB_UNKNOWN_EXPECTED"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	count := 0
	for scanner.Scan() {
		var row struct {
			Marker string
			ID     int64
			Value  any
		}
		if e = json.Unmarshal(scanner.Bytes(), &row); e != nil {
			t.Fatal(e)
		}
		var got any
		if row.Marker == "$behavior_code" {
			got, e = d.behaviorCode(context.Background(), row.ID)
		} else {
			got, e = d.signalObservation(context.Background(), row.ID)
		}
		if e != nil {
			t.Fatal(e)
		}
		encoded, e := json.Marshal(got)
		if e != nil {
			t.Fatal(e)
		}
		var actual any
		if e = json.Unmarshal(encoded, &actual); e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(actual, row.Value) {
			t.Fatalf("%s/%d mismatch got%v want%v", row.Marker, row.ID, actual, row.Value)
		}
		count++
	}
	if e = scanner.Err(); e != nil {
		t.Fatal(e)
	}
	if count == 0 {
		t.Fatal("empty proof")
	}
	t.Log("exact ROB definitions", count)
}
