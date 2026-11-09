package main

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

// cliFixture builds a small catalog and points the CLI at it.
func cliFixture(t *testing.T) {
	t.Helper()
	useCatalog(t, cliCatalog(t))
}

func useCatalog(t *testing.T, c *catalogtest.Catalog) {
	t.Helper()
	t.Setenv("TOYOTA_CATALOG", c.Path())
	t.Setenv("TOYOTA_REGION", "")
}

// openDocument opens a catalog holding one source document.
func openDocument(t *testing.T, member string, document map[string]any) *catalog.DB {
	t.Helper()
	c := catalogtest.New(t)
	c.Document(member, document)
	db, err := catalog.Open(c.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

// cliCatalog is the CLI fixture before it is finished.
func cliCatalog(t *testing.T) *catalogtest.Catalog {
	t.Helper()
	c := catalogtest.New(t)
	for _, e := range []catalogtest.ECU{
		{Category: 498, Key: "frc", Name: "Front Recognition Camera 2", Database: "FRC_P5.ddb", Generation: 20, SupportMode: "p5-standard", CatalogMember: "catalogs/NA/498.json", Counts: "{\"dids\":2,\"signals\":3,\"dtcs\":1,\"active_tests\":1}"},
		{Category: 405, Key: "eps", Name: "Power Steering", Database: "EMPS_P5.ddb", Generation: 20, SupportMode: "p5-standard", Counts: "{}"},
		{Category: 317, Key: "eps_317", Name: "EPS", Database: "EPS_P4.ddb", Generation: 4, SupportMode: "p4", Counts: "{}"},
	} {
		c.ECU(e)
	}
	c.Route(catalogtest.Route{Category: 498, Selector: 18, Request: 1938, Response: 1946, Transport: "iso15765-phase-family"})
	c.Install("NA", 8119, 498, 18, 0, true)
	c.Vehicle("NA", 12704, "Camry", 8119)
	c.Vehicle("NA", 12705, "Camry", 8120)
	c.Vehicle("NA", 12999, "Prius", 3)
	c.Vehicle("NA", 9999, "", 4)
	c.Item([]catalogtest.Item{
		{ID: 1, Category: 498, Kind: "did", Identifier: "0x1601", Name: "LTA Control Condition", Payload: json.RawMessage(`{"decoder":"p5-linear-msb0-v1","bit_start":8,"bit_end":15,"patterns":{"0":"OFF","1":"ON"}}`)},
		{ID: 2, Category: 498, Kind: "did", Identifier: "0x1601", Name: "LTA Switch Condition Flag", Payload: json.RawMessage(`{"decoder":"p5-linear-msb0-v1","bit_start":0,"bit_end":7,"unit":"flag"}`)},
		{ID: 3, Category: 498, Kind: "did", Identifier: "0x1202", Name: "LTA Installation Availability", Payload: json.RawMessage(`{"decoder":"p5-linear-msb0-v1","bit_start":13,"bit_end":13,"patterns":{"0":"Not Installed","1":"Installed"}}`)},
		{ID: 4, Category: 498, Kind: "dtc", Identifier: "U0131", Name: "Lost Communication with Power Steering", Detail: "No Signal", Payload: json.RawMessage(`{"code":"U013100","failure":"No Signal"}`)},
		{ID: 5, Category: 498, Kind: "active-test", Identifier: "0xA429", Name: "LTA Steering Vibration", Detail: "routine executable", Payload: json.RawMessage(`{"kind":"routine","execution":"executable"}`)},
		{ID: 6, Kind: "adu-signal", Identifier: "0x1B00", Name: "LTA main switch status", Detail: "ADAS recorder", Payload: json.RawMessage(`{}`)},
		{ID: 7, Kind: "did", Identifier: "0x9999", Name: "Didactic example signal"},
		{ID: 8, Kind: "did", Identifier: "0x8888", Name: "Software Version"},
	}...)
	return c
}

func output(t *testing.T, args ...string) string {
	t.Helper()
	out, err := capture(func() error { return run(args) })
	if err != nil {
		t.Fatalf("toyo %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func failure(t *testing.T, args ...string) error {
	t.Helper()
	_, err := capture(func() error { return run(args) })
	if err == nil {
		t.Fatalf("toyo %s should fail", strings.Join(args, " "))
	}
	return err
}

func TestECUShorthandAndExplicitFormsAgree(t *testing.T) {
	cliFixture(t)
	want := output(t, "frc", "data")
	for _, args := range [][]string{{"ecu", "frc", "data"}, {"ecu", "data", "frc"}, {"FRC", "signals"}, {"498", "dids"}} {
		if got := output(t, args...); got != want {
			t.Errorf("%v differs from toyo frc data:\n%s\nvs\n%s", args, got, want)
		}
	}
	if !strings.Contains(want, "0x1601") || strings.Contains(want, "U0131") {
		t.Fatal(want)
	}
}

func TestFamilyAliases(t *testing.T) {
	cliFixture(t)
	for alias, want := range map[string]string{"dtcs": "U0131", "dtc": "U0131", "codes": "U0131", "tests": "LTA Steering Vibration", "active-tests": "LTA Steering Vibration"} {
		if out := output(t, "frc", alias); !strings.Contains(out, want) {
			t.Errorf("frc %s lacks %q:\n%s", alias, want, out)
		}
	}
}

func TestIdentifierDecodesInPlace(t *testing.T) {
	cliFixture(t)
	single := output(t, "frc", "0x1202")
	for _, want := range []string{"LTA Installation Availability", "bit 13", "Not Installed"} {
		if !strings.Contains(single, want) {
			t.Errorf("missing %q\n%s", want, single)
		}
	}
	if again := output(t, "frc", "1202"); again != single {
		t.Error("hex prefix should not matter")
	}
	group := output(t, "frc", "0x1601")
	if !strings.Contains(group, "2 signals") || strings.Index(group, "0–7") > strings.Index(group, "8–15") {
		t.Fatalf("signals sharing a DID should list in bit order:\n%s", group)
	}
	if out := output(t, "inspect", "frc", "0x1202"); out != single {
		t.Error("inspect ECU IDENT should equal the shorthand")
	}
}

func TestUnknownWordsSearchInsteadOfDeadEnding(t *testing.T) {
	cliFixture(t)
	out := output(t, "lta")
	if !strings.Contains(out, "search") || !strings.Contains(out, "LTA Control Condition") {
		t.Fatalf("a bare word that is not an ECU should search:\n%s", out)
	}
	if out := output(t, "frc", "steering"); !strings.Contains(out, "LTA Steering Vibration") || strings.Contains(out, "main switch") {
		t.Fatalf("ECU + free text should search inside the ECU:\n%s", out)
	}
}

func TestSearchNearCommandNameIsNotStolen(t *testing.T) {
	cliFixture(t)
	out := output(t, "didactic", "example")
	if !strings.Contains(out, "Didactic example signal") || strings.Contains(out, "unknown command") {
		t.Fatalf("a query close to 'did' should still search:\n%s", out)
	}
	var miss *catalog.NotFoundError
	if err := failure(t, "serach", "lta"); !errors.As(err, &miss) || miss.What != "command" || miss.Suggestions[0] != "search" {
		t.Fatalf("multi-word command typo should still correct: %v", err)
	}
}

func TestVersionIsACatalogSearch(t *testing.T) {
	cliFixture(t)
	if out := output(t, "version"); !strings.Contains(out, "Software Version") {
		t.Fatalf("toyo version should search:\n%s", out)
	}
	err := failure(t, "version", "LTA")
	if strings.Contains(err.Error(), "takes no arguments") || strings.Contains(err.Error(), "unknown command") {
		t.Fatalf("toyo version LTA should be a search, not a subcommand: %v", err)
	}
	var miss *catalog.NotFoundError
	if !errors.As(err, &miss) {
		t.Fatalf("expected a search miss: %v", err)
	}
}

func TestInspectScopedQueryMatchesShorthand(t *testing.T) {
	cliFixture(t)
	want := output(t, "frc", "LTA", "control")
	if got := output(t, "inspect", "frc", "LTA", "control"); got != want {
		t.Fatalf("inspect ECU TEXT should match ECU shorthand:\n%s\nvs\n%s", got, want)
	}
}

func TestSearchFindsHexIdentifiersAndHintsECU(t *testing.T) {
	cliFixture(t)
	plain := output(t, "search", "1601")
	prefixed := output(t, "search", "0x1601")
	if !strings.Contains(plain, "LTA Control Condition") || !strings.Contains(prefixed, "LTA Control Condition") {
		t.Fatalf("1601:\n%s\n0x1601:\n%s", plain, prefixed)
	}
	if !strings.Contains(plain, "0x1601") {
		t.Fatal(plain)
	}
	for _, q := range []string{"frc", "FRC", "498", "Front Recognition Camera 2"} {
		out := output(t, "search", q)
		if !strings.Contains(out, "Nothing found") || !strings.Contains(out, "toyo frc opens") {
			t.Fatalf("search %q should hint at the ECU:\n%s", q, out)
		}
	}
	for _, q := range []string{"Front", "2"} {
		out := output(t, "search", q)
		if strings.Contains(out, "toyo frc opens") {
			t.Fatalf("search %q must not hint from a unique substring:\n%s", q, out)
		}
	}
	var items []catalog.Item
	if err := json.Unmarshal([]byte(output(t, "search", "frc", "--json")), &items); err != nil || len(items) != 0 {
		t.Fatalf("JSON search frc should stay an empty array: %v %d", err, len(items))
	}
	if err := json.Unmarshal([]byte(output(t, "search", "Front", "--json")), &items); err != nil || len(items) != 0 {
		t.Fatalf("JSON search Front should stay an empty array: %v %d", err, len(items))
	}
	hostile := output(t, "search", `name:LTA OR "1=1`)
	if !strings.Contains(hostile, "Nothing found") {
		t.Fatalf("operator-like punctuation should not become MATCH syntax:\n%s", hostile)
	}
}

func TestMistakesGetCorrections(t *testing.T) {
	cliFixture(t)
	var miss *catalog.NotFoundError
	if err := failure(t, "frcc"); !errors.As(err, &miss) || len(miss.Suggestions) == 0 || miss.Suggestions[0] != "frc" {
		t.Fatalf("typo of an ECU: %v", err)
	}
	if err := failure(t, "frcc", "data"); !errors.As(err, &miss) || miss.What != "ECU" {
		t.Fatalf("typo with an action should blame the ECU: %v", err)
	}
	if err := failure(t, "serach", "lta"); !errors.As(err, &miss) || miss.What != "command" || miss.Suggestions[0] != "search" {
		t.Fatalf("typo of a command: %v", err)
	}
	if err := failure(t, "inspect", "abc"); !isUsage(err, "inspect") {
		t.Fatalf("usage mistakes should exit 2 with the command named: %v", err)
	}
	if err := failure(t, "search"); !isUsage(err, "search") {
		t.Fatal(err)
	}
}

func isUsage(err error, command string) bool {
	var u *usageError
	return errors.As(err, &u) && u.command == command
}

func TestECUListLeadsWithDecodedAndSaysWhatIsHidden(t *testing.T) {
	cliFixture(t)
	out := output(t, "ecu")
	if !strings.Contains(out, "frc") || strings.Contains(out, "eps_317") || !strings.Contains(out, "2 more ECUs without decoded catalogs") {
		t.Fatalf("%s", out)
	}
	if all := output(t, "ecu", "--all"); !strings.Contains(all, "eps_317") {
		t.Fatalf("--all should include declared-only ECUs:\n%s", all)
	}
	if named := output(t, "ecu", "steering"); !strings.Contains(named, "eps") {
		t.Fatalf("a query falls back to declared ECUs when no decoded one matches:\n%s", named)
	}
}

func TestVehiclesGroupByName(t *testing.T) {
	cliFixture(t)
	list := output(t, "vehicle")
	if !strings.Contains(list, "Camry") || !strings.Contains(list, "2 variants") || !strings.Contains(list, "(unnamed)") {
		t.Fatal(list)
	}
	if strings.Count(list, "Camry") != 1 {
		t.Fatalf("a model should appear once:\n%s", list)
	}
	variants := output(t, "vehicle", "Camry")
	if !strings.Contains(variants, "12704") || !strings.Contains(variants, "12705") || !strings.Contains(variants, "1 ECU") {
		t.Fatal(variants)
	}
	detail := output(t, "vehicle", "12704")
	if !strings.Contains(detail, "frc") || !strings.Contains(detail, "0x792") {
		t.Fatal(detail)
	}
	if single := output(t, "vehicle", "Prius"); !strings.Contains(single, "type 12999") {
		t.Fatalf("a model with one variant opens directly:\n%s", single)
	}
}

func TestJSONContractsAreUnchangedByPresentation(t *testing.T) {
	cliFixture(t)
	var items []catalog.Item
	if err := json.Unmarshal([]byte(output(t, "search", "lta", "--json")), &items); err != nil || len(items) != 5 {
		t.Fatalf("search --json is a bare array: %v %d", err, len(items))
	}
	var envelope struct {
		ECU   catalog.ECU    `json:"ecu"`
		Items []catalog.Item `json:"items"`
		Shown int            `json:"shown"`
	}
	if err := json.Unmarshal([]byte(output(t, "frc", "data", "--json")), &envelope); err != nil || envelope.ECU.Key != "frc" || envelope.Shown != 3 {
		t.Fatalf("%v %+v", err, envelope)
	}
	var vehicles []catalog.Vehicle
	if err := json.Unmarshal([]byte(output(t, "vehicle", "--json")), &vehicles); err != nil || len(vehicles) != 4 {
		t.Fatalf("vehicle --json lists every type, ungrouped: %v %d", err, len(vehicles))
	}
	var one struct {
		Vehicle    catalog.Vehicle      `json:"vehicle"`
		Candidates []catalog.VehicleECU `json:"candidates"`
	}
	if err := json.Unmarshal([]byte(output(t, "vehicle", "12704", "--json")), &one); err != nil || len(one.Candidates) != 1 {
		t.Fatalf("%v %+v", err, one)
	}
	var multi struct {
		Records []catalog.ItemDetail `json:"records"`
	}
	if err := json.Unmarshal([]byte(output(t, "frc", "0x1601", "--json")), &multi); err != nil || len(multi.Records) != 2 {
		t.Fatalf("%v %+v", err, multi)
	}
}

func TestLimitSaysWhenThereIsMore(t *testing.T) {
	cliFixture(t)
	out := output(t, "search", "lta", "--limit", "2")
	if !strings.Contains(out, "first 2") || !strings.Contains(out, "--limit N shows more") {
		t.Fatal(out)
	}
	var items []catalog.Item
	if err := json.Unmarshal([]byte(output(t, "search", "lta", "--limit", "2", "--json")), &items); err != nil || len(items) != 2 {
		t.Fatalf("JSON must honour the limit exactly: %v %d", err, len(items))
	}
}

func TestCompletionKnowsTheCatalog(t *testing.T) {
	cliFixture(t)
	lines := func(args ...string) []string {
		return strings.Fields(strings.ReplaceAll(strings.ReplaceAll(output(t, append([]string{"__complete"}, args...)...), "\t", " "), "\n", "\n"))
	}
	first := func(args ...string) string { return strings.Join(lines(args...), " ") }
	if got := first("fr"); !strings.Contains(got, "frc") {
		t.Fatalf("ECU keys complete at the top level: %q", got)
	}
	if got := first(""); !strings.Contains(got, "search") || !strings.Contains(got, "monitor") {
		t.Fatalf("commands complete: %q", got)
	}
	if got := first("frc", "d"); !strings.Contains(got, "data") || !strings.Contains(got, "dtcs") || strings.Contains(got, "tests") {
		t.Fatalf("actions complete after an ECU: %q", got)
	}
	if got := first("--region", ""); !strings.Contains(got, "NA") || !strings.Contains(got, "EU") {
		t.Fatalf("%q", got)
	}
	if got := first("ecu", "info", "f"); !strings.Contains(got, "frc") {
		t.Fatalf("%q", got)
	}
	if got := first("vehicle", "ca"); !strings.Contains(got, "Camry") {
		t.Fatalf("%q", got)
	}
	if got := first("-"); !strings.Contains(got, "--json") || !strings.Contains(got, "--version") {
		t.Fatalf("%q", got)
	}
}

func TestCompletionScriptsAreEmitted(t *testing.T) {
	cliFixture(t)
	for shell, marker := range map[string]string{"zsh": "#compdef toyo", "bash": "complete -F _toyo toyo", "fish": "complete -c toyo"} {
		if out := output(t, "completion", shell); !strings.Contains(out, marker) || !strings.Contains(out, "__complete") {
			t.Errorf("%s script is wrong:\n%s", shell, out)
		}
	}
	if err := failure(t, "completion", "powershell"); !isUsage(err, "completion") {
		t.Fatal(err)
	}
}

func TestHelpIsPerCommandAndNeverNeedsACatalog(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	overviewText := output(t, "help")
	for _, want := range []string{"Explore", "Live", "toyo search TERM", "toyo monitor ECU"} {
		if !strings.Contains(overviewText, want) {
			t.Errorf("overview lacks %q\n%s", want, overviewText)
		}
	}
	for name := range nativeDocs {
		out := output(t, "help", name)
		if !strings.Contains(out, "toyo") || !strings.Contains(out, nativeDocs[name].summary) {
			t.Errorf("help %s:\n%s", name, out)
		}
	}
	if out := output(t, "frc", "--help"); !strings.Contains(out, "toyo ECU") {
		t.Fatalf("ECU shorthand help:\n%s", out)
	}
}

func TestEveryCommandWordIsRoutable(t *testing.T) {
	for _, name := range commandNames() {
		if name == "help" || nativeDocs[name].summary != "" {
			continue
		}
		if !runtimeCommand([]string{name}) {
			t.Errorf("%q is advertised as a command but nothing handles it", name)
		}
	}
}
