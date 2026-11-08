package main

import (
	"io"
	"os"
	"reflect"
	"strings"
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

func TestNativeHelpDoesNotRequireCatalog(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	for _, args := range [][]string{{"search", "--help"}, {"--region", "EU", "ecu", "--help"}, {"help", "inspect"}} {
		output, err := capture(func() error { return run(args) })
		if err != nil || !strings.Contains(output, "toyota") {
			t.Fatalf("%v: %q %v", args, output, err)
		}
	}
}

func TestLiveCommandHelpRendersFromGoTable(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	for _, args := range [][]string{{"help", "monitor"}, {"monitor", "--help"}, {"--help", "scan"}, {"health-check", "-h"}} {
		output, err := capture(func() error { return run(args) })
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if strings.Contains(output, "usage: toyota") {
			t.Fatalf("%v: argparse usage leaked through:\n%s", args, output)
		}
		if !strings.Contains(output, "toyota monitor") && !strings.Contains(output, "toyota scan") {
			t.Fatalf("%v: no house-style page:\n%s", args, output)
		}
	}
	for _, verb := range liveCommands {
		if _, ok := liveDocs[verb]; !ok {
			t.Fatalf("live command %q has no help doc", verb)
		}
	}
}

func TestBareHelpRequest(t *testing.T) {
	yes := [][]string{{"monitor", "--help"}, {"--help", "monitor"}, {"monitor", "-h"}}
	for _, args := range yes {
		if !bareHelpRequest(args, "monitor") {
			t.Fatalf("expected bare help: %q", args)
		}
	}
	no := [][]string{{"monitor", "frc", "--help"}, {"monitor", "--help", "--jsonl"}, {"monitor"}}
	for _, args := range no {
		if bareHelpRequest(args, "monitor") {
			t.Fatalf("not a bare help: %q", args)
		}
	}
}

func TestNativeRejectsUnknownOptions(t *testing.T) {
	if err := run([]string{"search", "LTA", "--limt", "10"}); err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatal(err)
	}
	if shouldBridge([]string{"--bogus", "frc"}) {
		t.Fatal("unknown option routed to Python")
	}
	if !shouldBridge([]string{"ecu", "frc", "ffd", "list"}) {
		t.Fatal("FFD shorthand intercepted")
	}
}

func TestRuntimeSharedOptionsAndAliases(t *testing.T) {
	for _, tc := range []struct{ input, want []string }{
		{[]string{"--json", "vehicle", "show", "--region", "EU"}, []string{"--region", "EU", "vehicle", "show", "--json"}},
		{[]string{"ecu", "read", "frc", "0x1601"}, []string{"ecu", "frc", "read", "0x1601"}},
	} {
		if got := runtimeArgs(tc.input); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%v != %v", got, tc.want)
		}
	}
}

func capture(fn func() error) (string, error) {
	original := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w
	defer func() { os.Stdout = original }()
	err := fn()
	w.Close()
	b, _ := io.ReadAll(r)
	r.Close()
	return string(b), err
}

func TestNativeAliasRequiresECU(t *testing.T) {
	for _, args := range [][]string{{"did", "list"}, {"dtc", "catalog"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "needs an ECU") {
			t.Fatalf("%v: %v", args, err)
		}
	}
}
func TestLiteralSearchOptions(t *testing.T) {
	opt, args, err := parse([]string{"search", "--", "--json"})
	if err != nil || opt.json || opt.literalAt != 1 || !reflect.DeepEqual(args, []string{"search", "--json"}) {
		t.Fatalf("%+v %v %v", opt, args, err)
	}
}
