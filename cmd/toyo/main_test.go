package main

import (
	"io"
	"os"
	"path/filepath"
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
	for _, args := range [][]string{{"search", "LTA"}, {"vehicle", "Camry"}, {"frc"}, {"frc", "data", "steering"}, {"ecu", "list"}, {"db", "status"}, {"browse"}, {"version"}, {"version", "LTA"}, {"version", "monitor"}} {
		if shouldBridge(args) {
			t.Fatalf("native catalog command was bridged: %q", args)
		}
	}
}

func TestNativeHelpDoesNotRequireCatalog(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	for _, args := range [][]string{{"search", "--help"}, {"--region", "EU", "ecu", "--help"}, {"help", "inspect"}} {
		output, err := capture(func() error { return run(args) })
		if err != nil || !strings.Contains(output, "toyo") {
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
		if strings.Contains(output, "usage: toyo") {
			t.Fatalf("%v: argparse usage leaked through:\n%s", args, output)
		}
		if !strings.Contains(output, "toyo monitor") && !strings.Contains(output, "toyo scan") {
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

func TestVersionAndUsageWorkWithoutCatalog(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	for _, args := range [][]string{{"--version"}, {"-V"}} {
		out, err := capture(func() error { return run(args) })
		if err != nil {
			t.Fatalf("%v: %v", args, err)
		}
		if !strings.HasPrefix(out, "toyo ") || strings.Contains(out, "v0.0.0-") {
			t.Fatalf("%v: %q", args, out)
		}
	}
	if _, _, err := parse([]string{"--version"}); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		args    []string
		want    string
		command string
	}{
		{[]string{"search"}, "search needs something to look for", "search"},
		{[]string{"inspect"}, "inspect needs a record number", "inspect"},
		{[]string{"inspect", "abc"}, "not a record number", "inspect"},
		{[]string{"browse", "--json"}, "browse is interactive", "browse"},
		{[]string{"completion"}, "zsh, bash or fish", "completion"},
		{[]string{"completion", "powershell"}, "unsupported shell", "completion"},
		{[]string{"--json"}, "--json needs a command", ""},
		{[]string{"serach", "lta"}, "unknown command", ""},
		{[]string{"searc", "lta"}, "unknown command", ""},
	}
	for _, tc := range cases {
		err := run(tc.args)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Fatalf("%v: %v", tc.args, err)
		}
		if tc.command != "" && !isUsage(err, tc.command) {
			t.Fatalf("%v should be usage for %s: %v", tc.args, tc.command, err)
		}
		if strings.Contains(err.Error(), "runtime catalog not found") {
			t.Fatalf("%v hid behind a missing catalog: %v", tc.args, err)
		}
	}

	out, err := capture(func() error { return run([]string{"completion", "zsh"}) })
	if err != nil || !strings.Contains(out, "#compdef toyo") {
		t.Fatalf("completion zsh should work without a catalog: %q %v", out, err)
	}
	t.Setenv("TOYOTA_REGION", "bogus")
	out, err = capture(func() error { return run([]string{"completion", "zsh"}) })
	if err != nil || !strings.Contains(out, "#compdef toyo") {
		t.Fatalf("completion zsh should ignore a bogus TOYOTA_REGION: %q %v", out, err)
	}
	t.Setenv("TOYOTA_REGION", "")

	for _, phrase := range [][]string{{"didactic", "example"}, {"customized", "settings"}, {"monitoring", "LTA"}, {"canbus", "foo"}, {"vehicles", "foo"}, {"version", "LTA"}} {
		err := run(phrase)
		if err == nil || strings.Contains(err.Error(), "unknown command") || strings.Contains(err.Error(), "takes no arguments") {
			t.Fatalf("%v should need a catalog, not be treated as a command: %v", phrase, err)
		}
		if !strings.Contains(err.Error(), "catalog not found") && !strings.Contains(err.Error(), "/does/not/exist") {
			t.Fatalf("%v: %v", phrase, err)
		}
	}

	for _, args := range [][]string{{"help", "frc"}, {"frc", "--help"}, {"help", "nosuchcommand"}} {
		out, err = capture(func() error { return run(args) })
		if err != nil || !strings.Contains(out, "toyo ECU") {
			t.Fatalf("%v should show generic ECU shorthand help: %q %v", args, out, err)
		}
	}
	_, err = capture(func() error { return run([]string{"help", "serach"}) })
	if err == nil || !strings.Contains(err.Error(), `unknown command "serach"`) {
		t.Fatalf("help serach should correct a command typo: %v", err)
	}

	for _, args := range [][]string{{"--version", "search"}, {"search", "--version"}} {
		if err := run(args); err == nil || !strings.Contains(err.Error(), "version takes no arguments") {
			t.Fatalf("%v: %v", args, err)
		}
	}
	if err := run([]string{"--version", "--bogus"}); err == nil || !strings.Contains(err.Error(), "unknown option") {
		t.Fatalf("--version --bogus: %v", err)
	}

	err = run([]string{"search", "LTA"})
	if err == nil || !strings.Contains(err.Error(), "/does/not/exist") || !strings.Contains(err.Error(), "--db PATH") {
		t.Fatalf("a missing --db path should name the file: %v", err)
	}
	t.Setenv("TOYOTA_CATALOG", "")
	err = run([]string{"search", "LTA"})
	if err == nil || !strings.Contains(err.Error(), "ghidra_rh850_analysis") || !strings.Contains(err.Error(), "--db PATH") {
		t.Fatalf("a missing catalog should say how to create or point at one: %v", err)
	}
}

func TestCatalogPathErrorsStayAccurate(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing.sqlite")
	err := run([]string{"--db", missing, "search", "LTA"})
	if err == nil || !strings.Contains(err.Error(), "catalog not found at") || !strings.Contains(err.Error(), missing) {
		t.Fatalf("missing file: %v", err)
	}
	err = run([]string{"--db", dir, "search", "LTA"})
	if err == nil || !strings.Contains(err.Error(), "is a directory") {
		t.Fatalf("directory: %v", err)
	}
	hidden := filepath.Join(dir, "hidden")
	if err := os.Mkdir(hidden, 0o700); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(hidden, "catalog.sqlite")
	if err := os.WriteFile(blocked, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(hidden, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(hidden, 0o700) })
	err = run([]string{"--db", blocked, "search", "LTA"})
	if err == nil || strings.Contains(err.Error(), "catalog not found") || !strings.Contains(err.Error(), "cannot read catalog") {
		t.Fatalf("permission should not look like a missing file: %v", err)
	}
}
