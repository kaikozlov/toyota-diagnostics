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
CREATE TABLE vehicles (region TEXT,vehicle_type INTEGER,name TEXT,install_set_ids TEXT);
INSERT INTO vehicles VALUES('NA',12704,'Camry HV','[1]');
CREATE TABLE vehicle_ecus (region TEXT, vehicle_type INTEGER, install_set_id INTEGER, category_id INTEGER, route_selector INTEGER, connection_frame_id INTEGER, route_key TEXT, request_address INTEGER, address_extension INTEGER, transport_kind TEXT);
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
INSERT INTO items VALUES(1,'NA',498,'did','0x1601','LTA Control Condition','');
INSERT INTO items VALUES(2,'NA',498,'dtc','U0131','Lost Communication with Power Steering','No Signal');
CREATE VIRTUAL TABLE items_fts USING fts5(name,detail,identifier,content='items',content_rowid='id',tokenize='unicode61',prefix='2 3 4');
INSERT INTO items_fts(items_fts) VALUES('rebuild');
CREATE TABLE item_payloads(item_id INTEGER PRIMARY KEY,data BLOB NOT NULL);`)
	if err != nil {
		t.Fatal(err)
	}
	for id, payload := range map[int]string{1: lta, 2: `{"code":"U013100","failure":"No Signal"}`} {
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
	m := newModel(fixture(t), "NA", "test")
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

func TestOpensOnDecodedECUDirectory(t *testing.T) {
	m := opened(t, 90, 24)
	if len(m.rows) != 1 || m.rows[0].ECU != "frc" {
		t.Fatalf("directory should list decoded ECUs only: %+v", m.rows)
	}
	if view := view(m); !strings.Contains(view, "Front Recognition Camera 2") || !strings.Contains(view, "1 data") {
		t.Fatal(view)
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
	m := opened(t, 90, 24)
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
	m = opened(t, 90, 24)
	m, cmd = press(m, tea.KeyEnter)
	m = settle(t, m, cmd)
	if m.scope == nil {
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
	if filters[m.filter].kind != "did" || len(m.rows) == 0 || len(m.rows) > before {
		t.Fatalf("filter=%v rows=%+v", filters[m.filter], m.rows)
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
	m := typed(t, opened(t, 130, 30), "l")
	m, cmd := press(m, tea.KeyDown)
	_ = cmd
	m, cmd = press(m, tea.KeyUp)
	if cmd == nil {
		t.Fatal("moving should request a preview")
	}
	msg := cmd()
	m, _ = update(m, msg)
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
