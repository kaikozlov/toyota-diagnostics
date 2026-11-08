package terminal

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

func detail(kind, ident, name, d, payload string) catalog.ItemDetail {
	return catalog.ItemDetail{
		Item: catalog.Item{ID: 7, Kind: kind, Region: "NA", CategoryID: 498, ECU: "frc", ECUName: "Front Recognition Camera 2",
			Identifier: ident, Name: name, Detail: d},
		Payload: json.RawMessage(payload),
	}
}

func plain(width int, fn func(Printer)) string { return Render(width, false, fn) }

func noOverflow(t *testing.T, out string, width int) {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > width {
			t.Errorf("line is %d wide, terminal is %d: %q", w, width, line)
		}
	}
}

func TestSearchRowsAlignAtEveryWidth(t *testing.T) {
	rows := []catalog.Item{
		{ID: 1, Kind: "did", ECU: "frc", Identifier: "0x1202", Name: "LTA Installation Availability"},
		{ID: 22, Kind: "dtc", ECU: "air_conditioner", Identifier: "B150E94", Name: "A very long trouble code name that must be cut somewhere reasonable", Detail: "Circuit Short to Battery or Open"},
		{ID: 333, Kind: "adu-signal", Identifier: "0x1B00", Name: "LTA main switch status(LTA)"},
	}
	for _, width := range []int{40, 60, 80, 120, 200} {
		out := plain(width, func(p Printer) { p.Search("lta", "NA", rows, false) })
		noOverflow(t, out, min(width, maxMeasure))
		if !strings.Contains(out, "LTA Installation Availability") && width >= 80 {
			t.Errorf("%d: name lost\n%s", width, out)
		}
	}
	wide := plain(120, func(p Printer) { p.Search("lta", "NA", rows, false) })
	if !strings.Contains(wide, "3 results") || !strings.Contains(wide, "#333") || !strings.Contains(wide, "—") {
		t.Fatal(wide)
	}
	more := plain(120, func(p Printer) { p.Search("lta", "NA", rows, true) })
	if !strings.Contains(more, "first 3") {
		t.Fatal("truncated results should say so:\n" + more)
	}
}

func TestUnfamiliarKindsStayAligned(t *testing.T) {
	rows := []catalog.Item{
		{ID: 1, Kind: "did", ECU: "frc", Identifier: "0x1202", Name: "Known kind"},
		{ID: 2, Kind: "future-kind-from-a-newer-catalog", ECU: "frc", Identifier: "0x1203", Name: "Kind from a newer catalog"},
	}
	out := plain(100, func(p Printer) { p.Search("kind", "NA", rows, false) })
	col := -1
	for _, line := range strings.Split(out, "\n") {
		if i := strings.Index(line, "frc"); i >= 0 {
			i = ansi.StringWidth(line[:i]) // columns, not bytes: the label ends in "…"
			if col >= 0 && i != col {
				t.Fatalf("columns drift when a kind label is long:\n%s", out)
			}
			col = i
		}
	}
	if !strings.Contains(out, "future-kind-f") {
		t.Fatal(out)
	}
}

func TestNoMatchesIsExplained(t *testing.T) {
	out := plain(80, func(p Printer) { p.Search("zzzz", "EU", nil, false) })
	if !strings.Contains(out, "no matches") || !strings.Contains(out, "--region") {
		t.Fatal(out)
	}
}

func TestSignalDetailDecodesTheRecord(t *testing.T) {
	it := detail("did", "0x1202", "LTA Installation Availability", "",
		`{"alternate_did":12802,"bit_start":13,"bit_end":13,"data_range":[0,1],"graph_range":[0,1],"decimal_point_count":0,
		  "decoder":"p5-linear-msb0-v1","div":1,"mul":1,"offset":0,"signed":false,"unit":null,
		  "patterns":{"1":"Installed","0":"Not Installed"}}`)
	out := plain(90, func(p Printer) { p.Item(it) })
	for _, want := range []string{"0x3202", "bit 13", "Not Installed", "Installed", "record #7", "p5-linear-msb0-v1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	if strings.Index(out, "Not Installed") > strings.Index(out, "1  Installed") {
		t.Errorf("values must be ordered numerically\n%s", out)
	}
	if strings.Contains(out, "raw value as is") || strings.Contains(out, "Graph") {
		t.Errorf("identity scaling and duplicate graph range are noise\n%s", out)
	}
	noOverflow(t, out, 90)
}

func TestSignalScalingFormula(t *testing.T) {
	it := detail("did", "0x1014", "IG Voltage", "", `{"decoder":"p5-linear-msb0-v1","bit_start":0,"bit_end":7,"mul":5,"div":2,"offset":-3,"decimal_point_count":1,"unit":"V","data_range":[0,255]}`)
	out := plain(90, func(p Printer) { p.Item(it) })
	if !strings.Contains(out, "raw × 5 ÷ 2 − 3, shown with 1 decimals") {
		t.Fatal(out)
	}
	unbounded := detail("did", "0x0102", "Time", "", `{"decoder":"p5-linear-msb0-v1","bit_start":0,"bit_end":31,"data_range":[0,-1]}`)
	if out := plain(90, func(p Printer) { p.Item(unbounded) }); strings.Contains(out, "0 … -1") {
		t.Fatalf("inverted range shown:\n%s", out)
	}
}

func TestP6SignalDetailStaysDecoded(t *testing.T) {
	it := detail("did", "0x1002", "BATT Voltage", "",
		`{"alternate_did":4098,"bit_end":15,"bit_start":0,"bit_width":16,"decimal_point_count":3,
		  "decoder":"p6-native-msb0-u32-v1","did_support":{"did":4098,"fixed_length":2},"div":512,
		  "extraction_mode":2,"flags":67,"graph_range":[0,79997],"list_member":true,"monitor_key":1,
		  "mul":625,"name":"BATT Voltage","offset":0,"pattern_display":{},"pattern_display_key":0,
		  "physical_data_key":115,"physical_range":[0.0,79.998],"primary_did":4098,"record":0,
		  "sign_mode":0,"signal_info":{"div":512},"sort_key":210,"source":"Engine_CM_P6.ddb",
		  "support_condition":null,"table":62,"unit":"V","unit_key":7,"visibility_override":0}`)
	out := plain(90, func(p Printer) { p.Item(it) })
	for _, want := range []string{"0x1002", "bits 0–15", "16 wide", "0 … 79.998", "V", "p6-native-msb0-u32-v1"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	for _, noise := range []string{"raw ×", "two's complement", "alternate", "Graph", "Signal info", "Mul", "Sort key"} {
		if strings.Contains(out, noise) {
			t.Errorf("P6 page carries %q noise\n%s", noise, out)
		}
	}
	noOverflow(t, out, 90)

	states := detail("did", "0x1004", "Drive Mode Status", "",
		`{"alternate_did":4100,"bit_end":7,"bit_start":0,"decoder":"p6-native-msb0-u32-v1",
		  "graph_range":[0,11],"physical_range":[0.0,11.0],"primary_did":4100,"sign_mode":2,"unit":null,
		  "pattern_display":{"0":"Normal","1":"Power","10":"Track","2":"Sport"}}`)
	out = plain(90, func(p Printer) { p.Item(states) })
	for _, want := range []string{"Sign mode", "2", "Normal", "Track", "Sport"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	if strings.Index(out, "Track") < strings.Index(out, "Sport") {
		t.Errorf("pattern keys must sort numerically\n%s", out)
	}
	noOverflow(t, out, 90)
}

func TestDTCSplitsCodeFromFailureByte(t *testing.T) {
	it := detail("dtc", "B150E94", "Instrument Panel Cluster (IPC) Control Module", "Unexpected Operation",
		`{"code":"B150E94","description":"Instrument Panel Cluster (IPC) Control Module","failure":"Unexpected Operation"}`)
	out := plain(100, func(p Printer) { p.Item(it) })
	for _, want := range []string{"B150E", "94  Unexpected Operation", "Body"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	// A degenerate identifier must not panic.
	plain(100, func(p Printer) { p.Item(detail("dtc", "", "x", "", `{}`)) })
}

func TestActiveTestSummarisesInsteadOfDumping(t *testing.T) {
	it := detail("active-test", "0xA429", "LTA Steering Vibration", "",
		`{"kind":"routine","execution":"plan_only","routine_id":5512,"positive_response":113,"session_requirement":"extended",
		  "start_frames":[{"x":1}],"stop_frames":[{"x":1},{"x":2}],"result_frames":[{"base_frame":{"identity":"/Users/someone/secret/path"}}]}`)
	out := plain(100, func(p Printer) { p.Item(it) })
	for _, want := range []string{"plan only", "0x1588", "0x71", "extended", "1 start frame", "2 stop frames"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "/Users/") {
		t.Fatalf("host paths from the generator must not surface:\n%s", out)
	}
}

func TestGenericRecordSummarisesStructures(t *testing.T) {
	it := detail("plugin", "0x41", "GetDatMonSignalInfoP5_DT.dll", "p5_signal_info",
		`{"dll":"GetDatMonSignalInfoP5_DT.dll","role":"p5_signal_info","occurrence":{"identity":"/Volumes/x","raw":"00"},"tags":["a","b"],"nothing":null,"empty":[]}`)
	out := plain(100, func(p Printer) { p.Item(it) })
	if !strings.Contains(out, "2 fields") || !strings.Contains(out, "a · b") || strings.Contains(out, "/Volumes") {
		t.Fatal(out)
	}
	if strings.Contains(out, "Nothing") || strings.Contains(out, "Empty") {
		t.Fatal("empty values should be omitted\n" + out)
	}
}

func TestDIDGroupListsSignalsByBit(t *testing.T) {
	ecu := catalog.ECU{Region: "NA", Key: "frc", Name: "Front Recognition Camera 2"}
	signals := []catalog.ItemDetail{
		detail("did", "0x1601", "LTA Control Condition", "", `{"bit_start":8,"bit_end":15,"patterns":{"0":"OFF","1":"ON"}}`),
		detail("did", "0x1601", "LTA Switch Condition Flag", "", `{"bit_start":0,"bit_end":7,"unit":"flag"}`),
	}
	out := plain(100, func(p Printer) { p.DID(ecu, "0x1601", signals) })
	for _, want := range []string{"2 signals", "8–15", "2 states", "flag", "#7"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q\n%s", want, out)
		}
	}
	noOverflow(t, out, 100)
}

func TestFailureOffersWayForward(t *testing.T) {
	var buf strings.Builder
	p := Printer{Out: &buf, Width: 80}
	p.Failure(&catalog.NotFoundError{What: "ECU", Query: "frcc", Suggestions: []string{"frc", "frs"}})
	got := buf.String()
	if !strings.HasPrefix(got, "toyo: no ECU matches \"frcc\"") || !strings.Contains(got, "did you mean frc, frs") {
		t.Fatal(got)
	}
	buf.Reset()
	p.Failure(&catalog.AmbiguousError{What: "ECU", Query: "EPS", Matches: []catalog.ECU{
		{Key: "eps_317", Name: "EPS", CategoryID: 317}, {Key: "eps", Name: "EPS", CategoryID: 405, CatalogMember: "x"},
	}})
	got = buf.String()
	lines := strings.Split(got, "\n")
	if !strings.Contains(got, "name one by key or category number") || len(lines) < 3 || !strings.HasPrefix(lines[1], "  eps ") || !strings.HasPrefix(lines[2], "  eps_317") {
		t.Fatalf("decoded ECUs should be listed first:\n%s", got)
	}
	buf.Reset()
	p.Failure(errors.New("plain failure"))
	if buf.String() != "toyo: plain failure\n" {
		t.Fatal(buf.String())
	}
}

func TestECUHidesEmptyFamiliesAndIsHonestAboutUndecoded(t *testing.T) {
	gen := 20
	e := catalog.ECU{Region: "NA", CategoryID: 498, Key: "frc", Name: "Front Recognition Camera 2", Database: "FRC_P5.ddb",
		Generation: &gen, SupportMode: "p5-standard", CatalogMember: "x",
		Counts: map[string]int{"signals": 283, "dids": 148, "dtcs": 58, "utilities": 0}}
	var buf strings.Builder
	Printer{Out: &buf, Width: 100}.ECU(e)
	out := buf.String()
	if !strings.Contains(out, "283") || !strings.Contains(out, "148 DIDs") || strings.Contains(out, "Utilities") ||
		!strings.Contains(out, "P5 · generation 20") || !strings.Contains(out, "toyo frc dtcs") {
		t.Fatal(out)
	}
	e.CatalogMember = ""
	out = plain(100, func(p Printer) { p.ECU(e) })
	if !strings.Contains(out, "No decoded catalog") || strings.Contains(out, "Data list") {
		t.Fatal(out)
	}
}

func TestMarkHighlightsOnlyWhenStyled(t *testing.T) {
	if got := plain(80, func(p Printer) { p.println(p.mark("LTA Control", []string{"lta"})) }); got != "LTA Control\n" {
		t.Fatalf("plain output must be untouched: %q", got)
	}
	styled := Render(80, true, func(p Printer) { p.println(p.mark("LTA Control", []string{"lta"})) })
	// Whether the test environment emits colour depends on the renderer's
	// detected profile; the text itself must always survive intact.
	if ansi.Strip(styled) != "LTA Control\n" {
		t.Fatalf("%q", styled)
	}
}

func TestHelpTablesShareAColumn(t *testing.T) {
	out := plain(100, func(p Printer) {
		p.Overview("tag", []HelpGroup{
			{Title: "A", Entries: []HelpEntry{{"toyo search TERM", "find"}}},
			{Title: "B", Entries: []HelpEntry{{"toyo x", "short"}}},
		}, nil)
	})
	col := -1
	for _, line := range strings.Split(out, "\n") {
		for _, word := range []string{"find", "short"} {
			if i := strings.Index(line, word); i >= 0 {
				if col >= 0 && i != col {
					t.Fatalf("summaries are not aligned:\n%s", out)
				}
				col = i
			}
		}
	}
}

func TestTermsSplitLikeAPersonReadsThem(t *testing.T) {
	got := Terms("steering-angle, 0x1601 (LTA)")
	want := []string{"steering-angle", "0x1601", "LTA"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("%v", got)
	}
}
