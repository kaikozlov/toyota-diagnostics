package main

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

func TestOperationListIsBounded(t *testing.T) {
	c := cliCatalog(t)
	for i := 9; i < 30; i++ {
		c.Item(catalogtest.Item{ID: int64(i), Category: 498, Kind: "active-test", Identifier: "0xA429", Name: "LTA Steering Vibration", Detail: "routine executable", Payload: map[string]any{"kind": "routine", "execution": "executable"}})
	}
	useCatalog(t, c)
	out := output(t, "active-test", "list", "--json", "--limit", "2")
	var rows []map[string]any
	if e := json.Unmarshal([]byte(out), &rows); e != nil {
		t.Fatal(e)
	}
	if len(rows) != 2 || len(out) > 4096 {
		t.Fatalf("unbounded listing: %d rows %d bytes", len(rows), len(out))
	}
	if rows[0]["catalog_item_id"] == nil || rows[0]["runtime_executable"] == nil || rows[0]["item"] != nil {
		t.Fatalf("%s", out)
	}
	text := output(t, "active-test", "list", "frc", "--limit", "2")
	if !strings.Contains(text, "toyo inspect ID") || strings.Count(text, "LTA Steering Vibration") != 2 {
		t.Fatal(text)
	}
}
func TestJ2534RejectsPandaMultiplexingWithoutConnection(t *testing.T) {
	r := &runtimeApp{ctx: context.Background(), opt: runtimeOptions{values: map[string][]string{"transport": {"j2534"}}, flags: map[string]bool{"obd-multiplexing": true}}}
	if e := r.connect(false); e == nil || !strings.Contains(e.Error(), "only supported by direct Panda") {
		t.Fatal(e)
	}
	if r.transport != nil {
		t.Fatal("opened transport")
	}
	if e := r.transportCommand([]string{"status"}); e == nil || !strings.Contains(e.Error(), "only supported by direct Panda") {
		t.Fatal(e)
	}
}
