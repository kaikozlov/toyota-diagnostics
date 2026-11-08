package explorer

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	_ "modernc.org/sqlite"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

func TestExplorerSearchInspectAndReturn(t *testing.T) {
	file := filepath.Join(t.TempDir(), "test.sqlite")
	db, err := sql.Open("sqlite", file)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
CREATE TABLE meta (key TEXT, value TEXT);
INSERT INTO meta VALUES('schema','toyota-diagnostics-sqlite-v1'),('release','test');
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT);
INSERT INTO ecus VALUES('NA',498,'frc','Front Recognition Camera 2');
INSERT INTO items VALUES(1,'NA',498,'did','0x1601','LTA Control Condition','');
CREATE VIRTUAL TABLE items_fts USING fts5(name,detail,identifier,content='items',content_rowid='id',tokenize='unicode61',prefix='2 3 4');
INSERT INTO items_fts(items_fts) VALUES('rebuild');`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()
	c, err := catalog.Open(file)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	m := newModel(c, "NA", "test")
	mm, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 25})
	m = mm.(model)
	for _, r := range "LTA" {
		mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = mm.(model)
	}
	if m.err != nil || len(m.rows) != 1 {
		t.Fatalf("search: rows=%d err=%v", len(m.rows), m.err)
	}
	if !strings.Contains(m.View(), "LTA Control Condition") {
		t.Fatal(m.View())
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = mm.(model)
	if !m.detail || !strings.Contains(m.View(), "Identifier") {
		t.Fatal("failed to enter detail")
	}
	mm, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mm.(model)
	if m.detail {
		t.Fatal("esc did not return to results")
	}
}

func TestTrimRespectsTerminalWidth(t *testing.T) {
	for _, sample := range []string{"long long long", "名前 がすごく長い", "abcdefg"} {
		got := trim(sample, 9)
		if got == "" {
			t.Fatal(sample)
		}
		if len([]rune(got)) > len([]rune(sample))+1 {
			t.Fatal(got)
		}
	}
}
