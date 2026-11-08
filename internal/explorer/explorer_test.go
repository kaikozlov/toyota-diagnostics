package explorer

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	_ "modernc.org/sqlite"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

const lta = `{"name":"LTA Control Condition","decoder":"p5-linear-msb0-v1","bit_start":0,"bit_end":7,"mul":1,"div":1,"offset":0,"unit":"degrees","patterns":{"0":"OFF","1":"ON"}}`

func fixture(t *testing.T) *catalog.DB {
	t.Helper()
	file := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE meta (key TEXT, value TEXT);
INSERT INTO meta VALUES('schema','toyota-diagnostics-sqlite-v2'),('release','test');
CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);
INSERT INTO ecus VALUES('NA',498,'frc','Front Recognition Camera 2','FRC_P5.ddb',20,'p5-standard','catalogs/NA/498.json','{"signals":1,"dtcs":1}');
INSERT INTO ecus VALUES('NA',405,'eps','Power Steering','EMPS_P5.ddb',20,'p5-standard',null,'{}');
INSERT INTO ecus VALUES('NA',80,'airbag','SRS Airbag','AIRBAG_P5.ddb',20,'p5-standard','catalogs/NA/80.json','{"dtcs":1}');
INSERT INTO ecus VALUES('NA',999,'blank','Empty Catalog Member','BLANK_P5.ddb',20,'p5-standard','catalogs/NA/999.json','{}');
INSERT INTO ecus VALUES('NA',60,'ds','Driving Support','DS_P5.ddb',20,'p5-standard','catalogs/NA/60.json','{"signals":1}');
CREATE TABLE vehicles (region TEXT,vehicle_type INTEGER,name TEXT,install_set_ids TEXT);
INSERT INTO vehicles VALUES('NA',12704,'Camry HV','[1]');
CREATE TABLE vehicle_ecus (region TEXT, vehicle_type INTEGER, install_set_id INTEGER, category_id INTEGER, route_selector INTEGER, connection_frame_id INTEGER, route_key TEXT, request_address INTEGER, address_extension INTEGER, transport_kind TEXT);
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
INSERT INTO items VALUES(1,'NA',498,'did','0x1601','LTA Control Condition','');
INSERT INTO items VALUES(2,'NA',498,'dtc','U0131','Lost Communication with Power Steering','No Signal');
INSERT INTO items VALUES(3,'NA',80,'dtc','B1650','Airbag Occupant Classification','');
INSERT INTO items VALUES(4,'NA',60,'did','0x6401','Driving Support Status','');
INSERT INTO items VALUES(5,'NA',60,'active-test-support-data-definitions','ATSDD-01','PCS Support Data Definitions','');
INSERT INTO items VALUES(6,'NA',60,'customize-group','CG-01','Air Conditioner Settings','');
INSERT INTO items VALUES(7,'NA',60,'customize-item','CI-01','Auto Recirculation','');
INSERT INTO items VALUES(8,'NA',60,'customize-probe','CP-01','Customize Probe','');
INSERT INTO items VALUES(9,'NA',60,'legacy-pid','0x0C','Legacy PID','');
INSERT INTO items VALUES(10,'NA',60,'legacy-did','0x0100','Legacy DID','');
INSERT INTO items VALUES(11,'NA',60,'legacy-freeze-frame','LFF-01','Legacy Freeze Frame','');
INSERT INTO items VALUES(12,'NA',60,'legacy-active-test','LAT-01','Legacy Active Test','');
INSERT INTO items VALUES(13,'NA',60,'rob-code','RC-01','ROB Code','');
INSERT INTO items VALUES(14,'NA',60,'rob-signal','RS-01','ROB Signal','');
INSERT INTO items VALUES(15,'NA',60,'generic-ffd-signal','0x1593','Generic FFD','');
INSERT INTO items VALUES(16,'NA',60,'group','GRP-01','Support Group','');
INSERT INTO items VALUES(17,'NA',60,'active-test-group','ATG-01','Active Test Group','');
INSERT INTO items VALUES(18,'NA',60,'ffd-signal','FFD-01','Freeze Frame Signal','');
INSERT INTO items VALUES(19,'NA',60,'active-test-source','ATS-01','Active Test Source','');
CREATE VIRTUAL TABLE items_fts USING fts5(name,detail,identifier,content='items',content_rowid='id',tokenize='unicode61',prefix='2 3 4');
INSERT INTO items_fts(items_fts) VALUES('rebuild');
CREATE TABLE item_payloads(item_id INTEGER PRIMARY KEY,data BLOB NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	payloads := map[int]string{1: lta, 2: `{"code":"U013100","failure":"No Signal"}`, 3: `{"code":"B165000"}`, 4: `{"name":"Driving Support Status"}`, 5: `{"name":"PCS Support Data Definitions"}`}
	for id := 6; id <= 19; id++ {
		payloads[id] = `{}`
	}
	for id, payload := range payloads {
		var buffer bytes.Buffer
		writer := zlib.NewWriter(&buffer)
		writer.Write([]byte(payload))
		writer.Close()
		if _, err := db.Exec("INSERT INTO item_payloads VALUES(?,?)", id, buffer.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	db.Close()
	c, err := catalog.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func view(m model) string { return m.View().Content }

func update(m model, msg tea.Msg) (model, tea.Cmd) {
	next, cmd := m.Update(msg)
	return next.(model), cmd
}

func press(m model, key rune) (model, tea.Cmd) { return update(m, tea.KeyPressMsg{Code: key}) }

func pressMsg(m model, msg tea.KeyPressMsg) (model, tea.Cmd) { return update(m, msg) }

// settle runs a command and feeds its message back, as the runtime would.
func settle(t *testing.T, m model, cmd tea.Cmd) model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	m, _ = update(m, cmd())
	return m
}

func opened(t *testing.T, width, height int) model {
	t.Helper()
	m := newModel(fixture(t), "NA")
	m, _ = update(m, tea.WindowSizeMsg{Width: width, Height: height})
	m.revision++
	m = settle(t, m, m.load())
	if m.err != nil {
		t.Fatal(m.err)
	}
	return m
}

func typed(t *testing.T, m model, text string) model {
	t.Helper()
	m.input.SetValue(text)
	m.revision++
	return settle(t, m, m.load())
}

func selectECU(t *testing.T, m model, key string) model {
	t.Helper()
	for i, row := range m.rows {
		if row.ECU == key {
			m.index = i
			return m
		}
	}
	t.Fatalf("missing ECU %s in %+v", key, m.rows)
	return m
}

func filterKinds(m model) []string {
	out := make([]string, len(m.filters))
	for i, f := range m.filters {
		out[i] = f.kind
	}
	return out
}

func clickAt(m model, x, y int) (model, tea.Cmd) {
	m, _ = update(m, tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
	return update(m, tea.MouseReleaseMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func TestOpensOnDecodedECUDirectory(t *testing.T) {
	m := opened(t, 90, 24)
	seen := map[string]bool{}
	for _, row := range m.rows {
		seen[row.ECU] = true
	}
	if !seen["frc"] || !seen["airbag"] || !seen["blank"] || !seen["ds"] {
		t.Fatalf("directory missing decoded ECUs: %+v", m.rows)
	}
	if seen["eps"] {
		t.Fatalf("declaration-only ECU should stay hidden: %+v", m.rows)
	}
	if view := view(m); !strings.Contains(view, "Front Recognition Camera 2") {
		t.Fatal(view)
	}
	if strings.Contains(view(m), "GTS+") || strings.Contains(view(m), "2026") {
		t.Fatalf("header should not brand the GTS+ release:\n%s", view(m))
	}
	if !strings.Contains(view(m), "toyota") || !strings.Contains(ansi.Strip(view(m)), "NA") {
		t.Fatalf("header should be the toyota product and region:\n%s", view(m))
	}
}

func TestSearchOpenDetailAndReturn(t *testing.T) {
	m := typed(t, opened(t, 90, 24), "LTA")
	if len(m.rows) != 1 || m.rows[0].Kind != "did" {
		t.Fatalf("%+v", m.rows)
	}
	m, cmd := press(m, tea.KeyEnter)
	if !m.detail || cmd == nil {
		t.Fatal("enter did not open detail")
	}
	m = settle(t, m, cmd)
	view := view(m)
	for _, want := range []string{"LTA Control Condition", "degrees", "OFF"} {
		if !strings.Contains(view, want) {
			t.Fatalf("detail lacks %q:\n%s", want, view)
		}
	}
	m, _ = press(m, tea.KeyEsc)
	if m.detail || m.input.Value() != "LTA" || len(m.rows) != 1 {
		t.Fatal("back lost the search")
	}
}

func TestEnterOnECUScopesAndBackspaceLeaves(t *testing.T) {
	m := selectECU(t, opened(t, 90, 24), "frc")
	m, cmd := press(m, tea.KeyEnter)
	if m.scope == nil || m.scope.Key != "frc" {
		t.Fatal("enter on an ECU should scope to it")
	}
	m = settle(t, m, cmd)
	if len(m.rows) != 2 {
		t.Fatalf("scoped listing should show both records: %+v", m.rows)
	}
	if !strings.Contains(view(m), "frc") || !strings.Contains(m.hints(), "esc back") {
		t.Fatal(view(m))
	}
	m, cmd = press(m, tea.KeyBackspace)
	if m.scope != nil {
		t.Fatal("backspace on empty input should leave the ECU")
	}
	m = settle(t, m, cmd)
	if m.rows[0].Kind != "ecu" {
		t.Fatal("should be back on the directory")
	}
}

func TestEscapeIsLayered(t *testing.T) {
	quits := func(cmd tea.Cmd) bool {
		if cmd == nil {
			return false
		}
		_, ok := cmd().(tea.QuitMsg)
		return ok
	}
	m := typed(t, opened(t, 90, 24), "LTA")
	m, cmd := press(m, tea.KeyEsc)
	if m.input.Value() != "" || quits(cmd) {
		t.Fatal("first esc should clear the query")
	}
	m = selectECU(t, opened(t, 90, 24), "frc")
	m, cmd = press(m, tea.KeyEnter)
	m = settle(t, m, cmd)
	if m.scope == nil || m.scope.Key != "frc" {
		t.Fatal("expected to be inside an ECU")
	}
	m, cmd = press(m, tea.KeyEsc)
	if m.scope != nil || quits(cmd) {
		t.Fatal("esc inside an ECU should step out, not quit")
	}
	if _, cmd := press(opened(t, 90, 24), tea.KeyEsc); !quits(cmd) {
		t.Fatal("esc at the top should quit")
	}
}

func TestFilterTabsNarrowResults(t *testing.T) {
	m := typed(t, opened(t, 90, 24), "control")
	before := len(m.rows)
	m, cmd := press(m, tea.KeyTab) // data
	m = settle(t, m, cmd)
	if m.filters[m.filter].kind != "did" || len(m.rows) == 0 || len(m.rows) > before {
		t.Fatalf("filter=%v rows=%+v", m.filters[m.filter], m.rows)
	}
	for _, r := range m.rows {
		if r.Kind != "did" {
			t.Fatalf("filter leaked %s", r.Kind)
		}
	}
	m, cmd = pressMsg(m, tea.KeyPressMsg{Code: tea.KeyTab, Mod: tea.ModShift})
	m = settle(t, m, cmd)
	if m.filter != 0 {
		t.Fatal("shift+tab should return to all")
	}
}

func TestSearchIsAsyncAndRejectsStaleResults(t *testing.T) {
	m := opened(t, 90, 24)
	m, cmd := update(m, tea.KeyPressMsg{Code: 'L', Text: "L"})
	if cmd == nil || !m.loading {
		t.Fatal("typing should start an async search")
	}
	old := m.revision
	m, _ = update(m, tea.KeyPressMsg{Code: 'T', Text: "T"})
	if _, cmd := update(m, searchDue(old)); cmd != nil {
		t.Fatal("stale debounce ran")
	}
	rows := len(m.rows)
	m, _ = update(m, listResult{revision: old, rows: []catalog.Item{{Name: "stale"}}})
	if len(m.rows) != rows {
		t.Fatal("stale result replaced the listing")
	}
	m, cmd = update(m, searchDue(m.revision))
	m = settle(t, m, cmd)
	if m.loading || len(m.rows) != 1 {
		t.Fatalf("current search missing: loading=%v rows=%d", m.loading, len(m.rows))
	}
}

func TestPreviewFollowsSelectionAndIgnoresLateAnswers(t *testing.T) {
	m := typed(t, opened(t, 130, 30), "LTA")
	if len(m.rows) != 1 || m.rows[0].Name != "LTA Control Condition" {
		t.Fatalf("%+v", m.rows)
	}
	cmd := m.loadPreview()
	if cmd == nil {
		t.Fatal("selection should load a preview")
	}
	m, _ = update(m, cmd())
	if !strings.Contains(view(m), "OFF") {
		t.Fatalf("wide layout should preview the record:\n%s", view(m))
	}
	// An answer for a record that is no longer selected must be dropped.
	m, _ = update(m, previewResult{key: "999|did||", data: previewData{}})
	if m.dataKey == "999|did||" {
		t.Fatal("late preview was accepted")
	}
}

func TestLayoutNeverOverflowsTheTerminal(t *testing.T) {
	for _, size := range [][2]int{{20, 8}, {45, 14}, {72, 20}, {109, 24}, {110, 24}, {160, 40}} {
		m := typed(t, opened(t, size[0], size[1]), "l")
		m, cmd := press(m, tea.KeyDown)
		if cmd != nil {
			m, _ = update(m, cmd())
		}
		m, cmd = press(m, tea.KeyUp)
		if cmd != nil {
			m, _ = update(m, cmd())
		}
		for _, mode := range []string{"list", "detail"} {
			if mode == "detail" {
				var open tea.Cmd
				m, open = press(m, tea.KeyEnter)
				if open != nil {
					m, _ = update(m, open())
				}
			}
			lines := strings.Split(view(m), "\n")
			if len(lines) > size[1]+1 {
				t.Errorf("%v %s: %d lines for height %d", size, mode, len(lines), size[1])
			}
			for _, line := range lines {
				if w := ansi.StringWidth(line); w > size[0] {
					t.Errorf("%v %s: line is %d wide: %q", size, mode, w, ansi.Strip(line))
				}
			}
		}
	}
}

func TestHeaderOmitsGTSRelease(t *testing.T) {
	plain := ansi.Strip(view(opened(t, 90, 24)))
	if strings.Contains(plain, "GTS+") || strings.Contains(plain, "GTS") {
		t.Fatalf("gts branding leaked into header:\n%s", plain)
	}
}

func TestRootKindFilterListsRecordsWithoutQuery(t *testing.T) {
	m := opened(t, 90, 24)
	m, cmd := press(m, tea.KeyTab) // data exists at root
	m = settle(t, m, cmd)
	if m.filters[m.filter].kind != "did" {
		t.Fatalf("expected data filter, got %v", filterKinds(m))
	}
	if len(m.rows) == 0 {
		t.Fatal("root data filter should list DIDs, not ask for a query")
	}
	for _, row := range m.rows {
		if row.Kind != "did" {
			t.Fatalf("data filter leaked %s", row.Kind)
		}
	}
	if strings.Contains(view(m), "Type to search") {
		t.Fatalf("should not treat a populated kind as empty:\n%s", view(m))
	}
}

func TestFiltersShowOnlyPresentKindsAndOmitEmptyECUScopes(t *testing.T) {
	m := opened(t, 90, 24)
	root := strings.Join(filterKinds(m), ",")
	for _, kind := range []string{"", "did", "dtc", "vehicle", "active-test-support-data-definitions"} {
		if !strings.Contains(","+root+",", ","+kind+",") {
			t.Fatalf("root filters missing %q: %s", kind, root)
		}
	}
	if strings.Contains(root, "plugin") {
		t.Fatalf("absent kind should not appear at root: %s", root)
	}
	m = selectECU(t, m, "frc")
	var cmd tea.Cmd
	m, cmd = press(m, tea.KeyEnter)
	m = settle(t, m, cmd)
	scoped := strings.Join(filterKinds(m), ",")
	if !strings.Contains(scoped, "did") || !strings.Contains(scoped, "dtc") {
		t.Fatalf("frc should keep its kinds: %s", scoped)
	}
	if strings.Contains(scoped, "vehicle") || strings.Contains(scoped, "active-test-support-data-definitions") {
		t.Fatalf("frc should not inherit kinds it does not have: %s", scoped)
	}

	m = selectECU(t, opened(t, 90, 24), "airbag")
	m, cmd = press(m, tea.KeyEnter)
	m = settle(t, m, cmd)
	airbag := strings.Join(filterKinds(m), ",")
	if strings.Contains(airbag, "did") {
		t.Fatalf("airbag has no data records: %s", airbag)
	}
	if !strings.Contains(airbag, "dtc") {
		t.Fatalf("airbag should expose dtc: %s", airbag)
	}

	m = selectECU(t, opened(t, 90, 24), "blank")
	m, cmd = press(m, tea.KeyEnter)
	m = settle(t, m, cmd)
	if len(m.filters) != 1 || m.filters[0].kind != "" {
		t.Fatalf("empty ECU should only offer all: %v", filterKinds(m))
	}
	if len(m.rows) != 0 || !strings.Contains(view(m), "No records in blank") {
		t.Fatalf("empty ECU should say so:\n%s", view(m))
	}
}

func TestFilterKeyboardIsNotTabOnly(t *testing.T) {
	m := opened(t, 90, 24)
	m, cmd := pressMsg(m, tea.KeyPressMsg{Code: tea.KeyRight})
	m = settle(t, m, cmd)
	if m.filters[m.filter].kind != "did" {
		t.Fatalf("right arrow should move to data: %v", filterKinds(m))
	}
	m, cmd = pressMsg(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	m = settle(t, m, cmd)
	if m.filter != 0 {
		t.Fatal("left arrow should return to all")
	}
	m = typed(t, opened(t, 90, 24), "LTA")
	before := m.filter
	m, _ = pressMsg(m, tea.KeyPressMsg{Code: tea.KeyLeft})
	if m.filter != before {
		t.Fatal("left arrow must edit a nonempty search, not steal it as a filter key")
	}
}

func TestMouseClickFilterAndPressReleaseSemantics(t *testing.T) {
	m := opened(t, 90, 24)
	var data, dtc filterChip
	for _, c := range m.chips {
		switch m.filters[c.index].kind {
		case "did":
			data = c
		case "dtc":
			dtc = c
		}
	}
	if data.x1 == 0 || dtc.x1 == 0 {
		t.Fatalf("missing chips: %+v", m.chips)
	}
	y := m.filterTop() + data.line
	plain := ansi.Strip(strings.Split(view(m), "\n")[y])
	if !strings.Contains(plain, "data") {
		t.Fatalf("visual filter row %d missing data: %q", y, plain)
	}
	x := strings.Index(plain, "data")
	if x < 0 || m.hitTest(x, y) != (hit{action: hitFilter, index: data.index}) {
		t.Fatalf("hit test missed data at %d,%d: %+v (chip=%+v plain=%q)", x, y, m.hitTest(x, y), data, plain)
	}
	m, cmd := clickAt(m, x, y)
	m = settle(t, m, cmd)
	if m.filters[m.filter].kind != "did" || len(m.rows) == 0 {
		t.Fatalf("clicking data should list DIDs: filter=%v rows=%+v", m.filters[m.filter], m.rows)
	}

	m = opened(t, 90, 24)
	m, _ = update(m, tea.MouseClickMsg{X: data.x0, Y: m.filterTop() + data.line, Button: tea.MouseLeft})
	m, cmd = update(m, tea.MouseReleaseMsg{X: dtc.x0, Y: m.filterTop() + dtc.line, Button: tea.MouseLeft})
	if cmd != nil {
		t.Fatal("press on one chip and release on another must not change the filter")
	}
	if m.filter != 0 {
		t.Fatalf("drag-off should keep all, got %v", m.filters[m.filter])
	}
}

func TestMouseRowSelectWheelAndOpen(t *testing.T) {
	m := opened(t, 90, 24)
	if len(m.rows) < 2 {
		t.Fatal(m.rows)
	}
	target := -1
	for i, row := range m.rows {
		if row.ECU == "frc" {
			target = i
			break
		}
	}
	h := m.hitTest(4, m.bodyTop()+target)
	if h.action != hitRow || h.index != target {
		// first visible row is index 0 when index is 0 and list is short
		h = hit{action: hitRow, index: target}
	}
	y := m.bodyTop()
	// Find the screen row for the target using the same windowing as the list.
	height := m.bodyHeight()
	start := max(0, m.index-height/2)
	start = max(0, min(start, len(m.rows)-height))
	y += target - start
	if m.hitTest(4, y) != (hit{action: hitRow, index: target}) {
		t.Fatalf("row hit %d,%d = %+v want row %d", 4, y, m.hitTest(4, y), target)
	}
	m, cmd := clickAt(m, 4, y)
	if m.index != target {
		t.Fatalf("click should select row %d, got %d", target, m.index)
	}
	if cmd != nil {
		m, _ = update(m, cmd())
	}
	m, cmd = clickAt(m, 4, y)
	if m.scope == nil || m.scope.Key != "frc" {
		t.Fatal("second click on the selected ECU should open it")
	}
	m = settle(t, m, cmd)

	m = opened(t, 90, 24)
	m, cmd = update(m, tea.MouseWheelMsg{X: 4, Y: m.bodyTop(), Button: tea.MouseWheelDown})
	if m.index != 1 {
		t.Fatalf("wheel should move the list, index=%d", m.index)
	}
	if cmd != nil {
		m, _ = update(m, cmd())
	}
}

func TestRegionCycleAndSearchNoResults(t *testing.T) {
	m := opened(t, 90, 24)
	key := tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl}
	if key.String() != "ctrl+r" {
		t.Fatalf("ctrl+r encoding changed: %q", key.String())
	}
	m, cmd := pressMsg(m, key)
	if m.region != "EU" {
		t.Fatalf("ctrl+r should cycle to EU, got %s (%q)", m.region, key.String())
	}
	m = settle(t, m, cmd)
	if m.region != "EU" {
		t.Fatalf("region=%s", m.region)
	}
	if len(m.rows) != 0 {
		t.Fatalf("EU fixture has no members: %+v", m.rows)
	}
	if !strings.Contains(view(m), "No ECUs with decoded catalogs in EU") {
		t.Fatalf("empty region message:\n%s", view(m))
	}
	if strings.Contains(view(m), "GTS+") {
		t.Fatal(view(m))
	}

	m = typed(t, opened(t, 90, 24), "zzz-no-such-record")
	if len(m.rows) != 0 || !strings.Contains(view(m), "Nothing matches") {
		t.Fatalf("expected no-result hint:\n%s", view(m))
	}
}

func TestFiltersWrapInsteadOfOverflow(t *testing.T) {
	m := opened(t, 40, 16)
	if len(m.chips) < 2 {
		t.Fatal(m.chips)
	}
	wrapped := false
	for _, c := range m.chips {
		if c.line > 0 {
			wrapped = true
		}
		if c.x1 > m.width {
			t.Fatalf("chip exceeds width: %+v width=%d", c, m.width)
		}
	}
	if !wrapped {
		t.Fatalf("expected wrapped chips at width 40: %+v", m.chips)
	}
	plain := ansi.Strip(view(m))
	if strings.Contains(plain, "active-test-support-data-definitions") {
		t.Fatalf("long kind should be compacted:\n%s", plain)
	}
}

func TestFilterLabelsDisambiguateCollapsedKindFamilies(t *testing.T) {
	m := opened(t, 90, 24)
	seen := map[string]string{}
	for _, f := range m.filters {
		if prev, ok := seen[f.label]; ok {
			t.Fatalf("chip label %q used for both %q and %q", f.label, prev, f.kind)
		}
		seen[f.label] = f.kind
	}
	plain := ansi.Strip(view(m))
	for _, stuck := range []string{"custom  custom", "legacy  legacy", "rob  rob"} {
		if strings.Contains(strings.Join(strings.Fields(plain), "  "), stuck) {
			t.Fatalf("collapsed family labels still visible: %q\n%s", stuck, plain)
		}
	}
	want := map[string]string{
		"customize-group":                      "cust-group",
		"customize-item":                       "cust-item",
		"customize-probe":                      "cust-probe",
		"legacy-pid":                           "legacy-pid",
		"legacy-did":                           "legacy-did",
		"rob-code":                             "rob-code",
		"rob-signal":                           "rob-signal",
		"ffd-signal":                           "ffd",
		"generic-ffd-signal":                   "generic-ffd",
		"group":                                "group",
		"active-test-group":                    "test-group",
		"active-test-source":                   "test-src",
		"active-test-support-data-definitions": "test-defs",
	}
	byKind := map[string]string{}
	for _, f := range m.filters {
		byKind[f.kind] = f.label
	}
	for kind, label := range want {
		if byKind[kind] != label {
			t.Fatalf("%s label=%q want %q (filters=%v)", kind, byKind[kind], label, byKind)
		}
	}
}

func TestEscBackRestoresECUSelection(t *testing.T) {
	m := selectECU(t, opened(t, 90, 24), "frc")
	if m.index == 0 {
		t.Fatal("fixture must not place frc first, or restore would be invisible")
	}
	m, cmd := press(m, tea.KeyEnter)
	m = settle(t, m, cmd)
	if m.scope == nil || m.scope.Key != "frc" {
		t.Fatal(m.scope)
	}
	m, cmd = press(m, tea.KeyEsc)
	m = settle(t, m, cmd)
	if m.scope != nil {
		t.Fatal("expected to leave the ECU")
	}
	if m.rows[m.index].ECU != "frc" {
		t.Fatalf("esc/back reset selection to %q (index %d)", m.rows[m.index].ECU, m.index)
	}
}

func TestWideLayoutGutterTracksTerminalWidth(t *testing.T) {
	small := opened(t, 120, 24)
	wide := opened(t, 200, 24)
	if small.listWidth() != 54 {
		t.Fatalf("120-col list width=%d", small.listWidth())
	}
	if wide.listWidth() != 90 {
		t.Fatalf("200-col list should grow with the window, width=%d", wide.listWidth())
	}
}
