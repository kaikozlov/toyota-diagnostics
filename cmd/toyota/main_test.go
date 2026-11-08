package main

import (
	"reflect"
	"testing"
)

func TestParseOptionsAnywhere(t *testing.T) {
	got, cmd, err := parse([]string{"search", "LTA", "--region", "EU", "--json", "--limit=12", "--db", "/tmp/catalog.sqlite"})
	if err != nil {
		t.Fatal(err)
	}
	if got.region != "EU" || got.limit != 12 || got.db != "/tmp/catalog.sqlite" || !got.json {
		t.Fatal(got)
	}
	if !reflect.DeepEqual(cmd, []string{"search", "LTA"}) {
		t.Fatal(cmd)
	}
	for _, args := range [][]string{{"--limit", "0"}, {"search", "LTA", "--limit", "abc"}, {"--region"}} {
		if _, _, err := parse(args); err == nil {
			t.Fatalf("accepted %q", args)
		}
	}
}

func TestBridgePreservesOriginalLiveCommands(t *testing.T) {
	for _, args := range [][]string{
		{"monitor", "frc", "LTA"}, {"ecu", "frc", "monitor", "LTA"},
		{"frc", "read", "0x1601"}, {"dtc", "scan"}, {"active-test", "run", "frc", "1", "--execute"},
		{"--vehicle", "12704", "frc"}, {"vehicle", "mounted"}, {"transport", "status"},
		{"uds", "raw", "0x763", "0x22", "1033"}, {"ffd", "image", "list"},
	} {
		_, cmd, err := parse(args)
		if err != nil {
			t.Fatal(err)
		}
		if !shouldBridge(cmd) {
			t.Fatalf("live command was intercepted: %q", args)
		}
	}
	for _, args := range [][]string{{"search", "LTA"}, {"vehicle", "Camry"}, {"frc"}, {"frc", "data", "steering"}, {"ecu", "list"}, {"db", "status"}, {"browse"}} {
		if shouldBridge(args) {
			t.Fatalf("native catalog command was bridged: %q", args)
		}
	}
}
