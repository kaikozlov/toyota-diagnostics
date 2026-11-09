package catalog

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestCustomizeCorpusProof(t *testing.T) {
	path := os.Getenv("TOYO_CUSTOMIZE_PROOF")
	if path == "" {
		t.Skip("optional independent producer corpus")
	}
	db, e := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	d := &DB{db: db}
	f, e := os.Open(os.Getenv("TOYO_CUSTOMIZE_EXPECTED"))
	if e != nil {
		t.Fatal(e)
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 4096), 1024*1024)
	count := 0
	batchNodes := map[string][]any{}
	batchExpected := map[string][]any{}
	for scanner.Scan() {
		var row struct {
			Marker string
			ID     int64
			Value  map[string]any
		}
		decoder := json.NewDecoder(strings.NewReader(scanner.Text()))
		decoder.UseNumber()
		if e = decoder.Decode(&row); e != nil {
			t.Fatal(e)
		}
		var got any
		if row.Marker == "$customize_item" {
			got, e = d.customizeItem(context.Background(), row.ID)
		} else {
			got, e = d.customizeGroup(context.Background(), row.ID)
		}
		if e != nil {
			t.Fatal(e)
		}
		if !reflect.DeepEqual(got, row.Value) {
			t.Fatalf("row %d mismatch\ngot%v\nwant%v", count, got, row.Value)
		}
		batchNodes[row.Marker] = append(batchNodes[row.Marker], map[string]any{row.Marker: json.Number(fmt.Sprint(row.ID))})
		batchExpected[row.Marker] = append(batchExpected[row.Marker], row.Value)
		count++
	}
	if e = scanner.Err(); e != nil {
		t.Fatal(e)
	}
	if count == 0 {
		t.Fatal("empty proof")
	}
	for marker, nodes := range batchNodes {
		got, err := d.expandMetadata(context.Background(), nodes, referenceStack{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, batchExpected[marker]) {
			t.Fatalf("batch mismatch %s", marker)
		}
		if marker == "$customize_item" {
			row := got.([]any)[0].(map[string]any)
			choices := row["choices"].([]any)
			if len(choices) > 0 {
				choices[0].(map[string]any)["name"] = "mutated"
			}
		}
	}
	t.Logf("exact point and batch customize rows: %d", count)
}
func TestCustomizeMissing(t *testing.T) {
	d := emptyDB(t)
	_, e := d.db.Exec("CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT)")
	if e != nil {
		t.Fatal(e)
	}
	cols := []string{"id INTEGER PRIMARY KEY"}
	for _, f := range customizeFields(customizeGroupNames, false) {
		cols = append(cols, f.column+" INTEGER")
	}
	if _, e = d.db.Exec("CREATE TABLE customize_group_definitions(" + strings.Join(cols, ",") + ")"); e != nil {
		t.Fatal(e)
	}
	if _, e = d.customizeGroup(context.Background(), 1); e == nil {
		t.Fatal("missing group accepted")
	}
}
