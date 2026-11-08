// Package explorer is the keyboard-first catalog browser. It opens on a
// browsable ECU directory, searches as you type, drills into one ECU, and
// previews the highlighted record beside the list.
package explorer

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
)

const (
	resultLimit  = 300
	debounce     = 60 * time.Millisecond
	previewWidth = 110 // terminals at least this wide get the side preview
	chipMaxWidth = 16
)

var regions = []string{"NA", "EU", "JP"}

type kindFilter struct{ kind, label string }

// preferredFilters is the display order for well-known families. Only kinds
// that exist in the current scope are shown.
var preferredFilters = []kindFilter{
	{"did", "data"}, {"dtc", "dtc"}, {"active-test", "test"}, {"ecu", "ecu"},
	{"vehicle", "vehicle"}, {"function", "func"}, {"plugin", "plugin"}, {"utility", "utility"},
	{"ffd-signal", "ffd"}, {"adu-signal", "adu"},
}

type searchDue uint64

type listResult struct {
	revision uint64
	rows     []catalog.Item
	err      error
}

// previewData is what a row resolves to; it is rendered at whatever width the
// current layout has, so resizing never needs another query.
type previewData struct {
	item    *catalog.ItemDetail
	ecu     *catalog.ECU
	vehicle *catalog.Vehicle
	fleet   []catalog.VehicleECU
	err     error
}

type previewResult struct {
	key  string
	data previewData
}

type filterChip struct {
	index        int
	label        string
	line, x0, x1 int
}

type hitAction int

const (
	hitNone hitAction = iota
	hitFilter
	hitRow
	hitPreview
	hitBack
)

type hit struct {
	action hitAction
	index  int
}

type model struct {
	db      *catalog.DB
	region  string
	input   textinput.Model
	scope   *catalog.ECU
	filter  int
	filters []kindFilter
	chips   []filterChip

	rows     []catalog.Item
	index    int
	revision uint64
	loading  bool
	err      error

	width, height int

	data       previewData
	dataKey    string
	preview    string
	previewTop int

	detail    bool
	viewport  viewport.Model
	mouseDown hit
}

func newModel(db *catalog.DB, region string) model {
	input := textinput.New()
	input.Prompt = "/ "
	styles := input.Styles()
	styles.Focused.Prompt = terminal.StyleAccent
	styles.Blurred.Prompt = terminal.StyleAccent
	input.SetStyles(styles)
	input.CharLimit = 180
	input.Focus()
	m := model{db: db, region: region, input: input, width: 100, height: 28, viewport: viewport.New(viewport.WithWidth(96), viewport.WithHeight(20))}
	m.loadFilters()
	m.layout()
	return m
}

func (m *model) loadFilters() {
	prev := ""
	if m.filter >= 0 && m.filter < len(m.filters) {
		prev = m.filters[m.filter].kind
	}
	category := 0
	if m.scope != nil {
		category = m.scope.CategoryID
	}
	kinds, err := m.db.Kinds(context.Background(), m.region, category)
	if err != nil {
		m.err = err
		kinds = nil
	}
	present := make(map[string]bool, len(kinds))
	for _, kind := range kinds {
		present[kind] = true
	}
	hasVehicle := false
	if m.scope == nil {
		if vehicles, err := m.db.Vehicles(context.Background(), m.region, "", 1); err == nil {
			hasVehicle = len(vehicles) > 0
		}
	}

	next := []kindFilter{{"", "all"}}
	seen := map[string]bool{"": true}
	for _, f := range preferredFilters {
		if f.kind == "vehicle" {
			if hasVehicle {
				next = append(next, f)
				seen[f.kind] = true
			}
			continue
		}
		if present[f.kind] {
			if m.scope != nil && (f.kind == "ecu" || f.kind == "vehicle") {
				continue
			}
			next = append(next, f)
			seen[f.kind] = true
		}
	}
	for _, kind := range kinds {
		if seen[kind] || kind == "" {
			continue
		}
		if m.scope != nil && (kind == "ecu" || kind == "vehicle") {
			continue
		}
		next = append(next, kindFilter{kind, compactLabel(terminal.KindLabel(kind), chipMaxWidth)})
	}
	m.filters = next
	m.filter = 0
	for i, f := range m.filters {
		if f.kind == prev {
			m.filter = i
			break
		}
	}
}

func compactLabel(label string, width int) string {
	label = strings.TrimSpace(label)
	if width < 1 {
		width = 1
	}
	if ansi.StringWidth(label) <= width {
		return label
	}
	return ansi.Truncate(label, width, "…")
}

func (m model) Init() tea.Cmd { return tea.Batch(textinput.Blink, m.load()) }

// ----- data loading -------------------------------------------------------

func (m model) query() string { return strings.TrimSpace(m.input.Value()) }

func (m model) load() tea.Cmd {
	revision, db, region, scope, kind, query := m.revision, m.db, m.region, m.scope, m.filters[m.filter].kind, m.query()
	return func() tea.Msg {
		rows, err := fetch(context.Background(), db, region, scope, kind, query)
		return listResult{revision, rows, err}
	}
}

func fetch(ctx context.Context, db *catalog.DB, region string, scope *catalog.ECU, kind, query string) ([]catalog.Item, error) {
	switch {
	case query != "" && scope != nil:
		return db.SearchKind(ctx, region, scope.CategoryID, kind, query, resultLimit)
	case query != "":
		return db.SearchKind(ctx, region, 0, kind, query, resultLimit)
	case scope != nil:
		return db.ECURecords(ctx, *scope, kind, resultLimit)
	case kind == "" || kind == "ecu":
		ecus, err := db.ListECUs(ctx, region, "", true, 5000)
		if err != nil {
			return nil, err
		}
		rows := make([]catalog.Item, len(ecus))
		for i, e := range ecus {
			rows[i] = catalog.Item{Kind: "ecu", Region: e.Region, CategoryID: e.CategoryID, ECU: e.Key, ECUName: e.Name,
				Identifier: strconv.Itoa(e.CategoryID), Name: e.Name, Detail: stats(e)}
		}
		return rows, nil
	case kind == "vehicle":
		vehicles, err := db.Vehicles(ctx, region, "", 20000)
		if err != nil {
			return nil, err
		}
		rows := make([]catalog.Item, len(vehicles))
		for i, v := range vehicles {
			rows[i] = catalog.Item{Kind: "vehicle", Region: v.Region, Identifier: strconv.Itoa(v.Type), Name: v.Name}
		}
		return rows, nil
	}
	return db.ListItems(ctx, region, 0, kind, resultLimit)
}

func stats(e catalog.ECU) string {
	var parts []string
	for _, s := range []struct{ key, label string }{{"signals", "data"}, {"dtcs", "dtc"}, {"active_tests", "tests"}} {
		if n := e.Counts[s.key]; n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n, s.label))
		}
	}
	return strings.Join(parts, " · ")
}

func (m model) selection() (catalog.Item, bool) {
	if m.index < 0 || m.index >= len(m.rows) {
		return catalog.Item{}, false
	}
	return m.rows[m.index], true
}

func keyOf(it catalog.Item) string {
	return fmt.Sprintf("%d|%s|%s|%s", it.ID, it.Kind, it.ECU, it.Identifier)
}

func (m model) selectionKey() string {
	if it, ok := m.selection(); ok {
		return keyOf(it)
	}
	return ""
}

// loadPreview resolves the highlighted row; stale answers are discarded by key.
func (m model) loadPreview() tea.Cmd {
	it, ok := m.selection()
	if !ok {
		return nil
	}
	db := m.db
	return func() tea.Msg {
		ctx := context.Background()
		var d previewData
		switch {
		case it.Kind == "ecu" && it.ECU != "":
			e, err := db.ECU(ctx, it.Region, it.ECU)
			d = previewData{ecu: &e, err: err}
		case it.Kind == "vehicle":
			d = vehiclePreview(ctx, db, it)
		default:
			item, err := db.Item(ctx, it.ID)
			d = previewData{item: &item, err: err}
		}
		return previewResult{keyOf(it), d}
	}
}

func vehiclePreview(ctx context.Context, db *catalog.DB, it catalog.Item) previewData {
	matches, err := db.Vehicles(ctx, it.Region, it.Identifier, 20)
	if err != nil {
		return previewData{err: err}
	}
	for _, v := range matches {
		if strconv.Itoa(v.Type) == it.Identifier {
			fleet, err := db.VehicleECUs(ctx, v)
			return previewData{vehicle: &v, fleet: fleet, err: err}
		}
	}
	return previewData{err: fmt.Errorf("vehicle type %s not found", it.Identifier)}
}

// ----- layout ---------------------------------------------------------------

func (m model) wide() bool { return m.width >= previewWidth }

func (m model) filterLineCount() int {
	if n := len(m.filterLines()); n > 0 {
		return n
	}
	return 1
}

func (m model) filterLines() []int {
	seen := map[int]bool{}
	var lines []int
	for _, c := range m.chips {
		if !seen[c.line] {
			seen[c.line] = true
			lines = append(lines, c.line)
		}
	}
	return lines
}

func (m model) chromeHeight() int {
	// title+blank, search, filter rows, blank, hints
	return 5 + m.filterLineCount()
}

func (m model) bodyHeight() int { return max(1, m.height-m.chromeHeight()) }

func (m model) listWidth() int {
	if !m.wide() {
		return max(10, m.width-4)
	}
	return min(max(m.width*45/100, 52), 72)
}

func (m model) previewWidth() int { return max(10, m.width-m.listWidth()-8) }

func (m model) searchY() int { return 2 }

func (m model) filterTop() int { return 3 }

func (m model) bodyTop() int { return m.filterTop() + m.filterLineCount() + 1 }

func (m *model) layout() {
	m.width, m.height = max(20, m.width), max(8, m.height)
	crumb := 0
	if m.scope != nil {
		crumb = lipgloss.Width(m.scope.Key) + 3
	}
	m.input.SetWidth(max(8, min(90, m.width-8-crumb)))
	m.viewport.SetWidth(max(10, m.width-4))
	m.viewport.SetHeight(max(1, m.height-5))
	m.input.Placeholder = "Search signals, DTCs, tests, ECUs, vehicles…"
	if m.scope != nil {
		m.input.Placeholder = "Search inside " + m.scope.Key + "…"
	}
	m.layoutFilters()
	m.renderPreview()
}

func (m *model) layoutFilters() {
	width := max(8, m.width)
	inner := max(1, width-2)
	m.chips = m.chips[:0]
	line, x := 0, 1
	for i, f := range m.filters {
		label := compactLabel(f.label, min(chipMaxWidth, inner))
		w := ansi.StringWidth(label)
		if x > 1 && x+w > width {
			line++
			x = 1
		}
		if x+w > width {
			label = compactLabel(label, max(1, width-x))
			w = ansi.StringWidth(label)
		}
		m.chips = append(m.chips, filterChip{index: i, label: label, line: line, x0: x, x1: x + w})
		x += w + 2
	}
}

func (m *model) renderPreview() {
	if m.dataKey == "" || m.dataKey != m.selectionKey() {
		m.preview = ""
		return
	}
	m.preview = renderData(m.data, m.previewWidth())
	if m.detail {
		m.viewport.SetContent(indent(renderData(m.data, m.viewport.Width()-2)))
	}
}

func indent(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i := range lines {
		lines[i] = "  " + lines[i]
	}
	return strings.Join(lines, "\n")
}

func renderData(d previewData, width int) string {
	if d.err != nil {
		return terminal.StyleError.Render(d.err.Error())
	}
	return terminal.Render(width, true, func(p terminal.Printer) {
		switch {
		case d.ecu != nil:
			p.ECU(*d.ecu)
		case d.vehicle != nil:
			p.Vehicle(*d.vehicle, d.fleet)
		case d.item != nil:
			p.Item(*d.item)
		}
	})
}

// ----- update ---------------------------------------------------------------

// refresh starts a new listing. Typing is debounced; deliberate changes
// (filter, scope, region) are not.
func (m model) refresh(typed bool) (model, tea.Cmd) {
	m.revision++
	m.loading = true
	if typed {
		revision := m.revision
		return m, tea.Tick(debounce, func(time.Time) tea.Msg { return searchDue(revision) })
	}
	return m, m.load()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case searchDue:
		if uint64(msg) == m.revision {
			return m, m.load()
		}
		return m, nil
	case listResult:
		if msg.revision != m.revision {
			return m, nil
		}
		m.rows, m.err, m.loading, m.index, m.previewTop = msg.rows, msg.err, false, 0, 0
		m.dataKey, m.preview = "", ""
		return m, m.loadPreview()
	case previewResult:
		if msg.key != m.selectionKey() {
			return m, nil
		}
		m.data, m.dataKey = msg.data, msg.key
		m.renderPreview()
		return m, nil
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.layout()
		return m, nil
	case tea.KeyPressMsg:
		return m.key(msg)
	case tea.MouseClickMsg:
		return m.mousePress(msg.Mouse())
	case tea.MouseReleaseMsg:
		return m.mouseRelease(msg.Mouse())
	case tea.MouseWheelMsg:
		return m.mouseWheel(msg.Mouse())
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m model) move(delta int) (model, tea.Cmd) {
	if len(m.rows) == 0 {
		return m, nil
	}
	return m.moveTo(max(0, min(len(m.rows)-1, m.index+delta)))
}

func (m model) moveTo(next int) (model, tea.Cmd) {
	if next < 0 || next >= len(m.rows) || next == m.index {
		return m, nil
	}
	m.index, m.previewTop = next, 0
	m.dataKey, m.preview = "", ""
	return m, m.loadPreview()
}

func (m model) shiftFilter(delta int) (model, tea.Cmd) {
	if len(m.filters) == 0 {
		return m, nil
	}
	n := len(m.filters)
	m.filter = (m.filter + delta%n + n) % n
	return m.refresh(false)
}

func (m model) key(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if key == "ctrl+c" {
		return m, tea.Quit
	}
	if m.detail {
		switch key {
		case "esc", "enter", "q", "backspace":
			m.detail = false
			return m, m.input.Focus()
		}
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	page := max(1, m.bodyHeight()-1)
	switch key {
	case "esc":
		switch {
		case m.query() != "":
			m.input.SetValue("")
			return m.refresh(false)
		case m.scope != nil:
			return m.leaveScope()
		}
		return m, tea.Quit
	case "up", "ctrl+p":
		return m.move(-1)
	case "down", "ctrl+n":
		return m.move(1)
	case "pgup":
		return m.move(-page)
	case "pgdown":
		return m.move(page)
	case "shift+up":
		m.previewTop = max(0, m.previewTop-3)
		return m, nil
	case "shift+down":
		m.previewTop += 3
		return m, nil
	case "tab":
		return m.shiftFilter(1)
	case "shift+tab":
		return m.shiftFilter(-1)
	case "left":
		if m.input.Value() == "" {
			return m.shiftFilter(-1)
		}
	case "right":
		if m.input.Value() == "" {
			return m.shiftFilter(1)
		}
	case "ctrl+r":
		for i, r := range regions {
			if r == m.region {
				m.region = regions[(i+1)%len(regions)]
				break
			}
		}
		m.scope = nil
		m.loadFilters()
		m.layout()
		return m.refresh(false)
	case "backspace":
		if m.input.Value() == "" && m.scope != nil {
			return m.leaveScope()
		}
	case "enter":
		return m.openSelection()
	}
	before := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != before {
		m, refreshCmd := m.refresh(m.query() != "")
		return m, tea.Batch(cmd, refreshCmd)
	}
	return m, cmd
}

func (m model) openSelection() (model, tea.Cmd) {
	it, ok := m.selection()
	if !ok {
		return m, nil
	}
	if it.Kind == "ecu" && it.ECU != "" {
		e, err := m.db.ECU(context.Background(), it.Region, it.ECU)
		if err != nil {
			m.err = err
			return m, nil
		}
		m.scope = &e
		m.input.SetValue("")
		m.filter = 0
		m.loadFilters()
		m.layout()
		return m.refresh(false)
	}
	m.detail = true
	m.viewport.GotoTop()
	if m.dataKey == keyOf(it) {
		m.renderPreview()
		return m, nil
	}
	m.viewport.SetContent("  Loading…")
	return m, m.loadPreview()
}

func (m model) leaveScope() (model, tea.Cmd) {
	m.scope = nil
	m.filter = 0
	m.loadFilters()
	m.layout()
	return m.refresh(false)
}

func (m model) mousePress(ev tea.Mouse) (model, tea.Cmd) {
	if ev.Button != tea.MouseLeft {
		return m, nil
	}
	m.mouseDown = m.hitTest(ev.X, ev.Y)
	return m, nil
}

func (m model) mouseRelease(ev tea.Mouse) (model, tea.Cmd) {
	if ev.Button != tea.MouseLeft {
		return m, nil
	}
	down := m.mouseDown
	m.mouseDown = hit{}
	up := m.hitTest(ev.X, ev.Y)
	if down.action == hitNone || down.action != up.action || down.index != up.index {
		return m, nil
	}
	return m.activate(up)
}

func (m model) mouseWheel(ev tea.Mouse) (model, tea.Cmd) {
	delta := 0
	switch ev.Button {
	case tea.MouseWheelUp:
		delta = -1
	case tea.MouseWheelDown:
		delta = 1
	default:
		return m, nil
	}
	if m.detail {
		if delta < 0 {
			m.viewport.ScrollUp(3)
		} else {
			m.viewport.ScrollDown(3)
		}
		return m, nil
	}
	if m.hitTest(ev.X, ev.Y).action == hitPreview {
		if delta < 0 {
			m.previewTop = max(0, m.previewTop-3)
		} else {
			m.previewTop += 3
		}
		return m, nil
	}
	return m.move(delta)
}

func (m model) activate(h hit) (model, tea.Cmd) {
	switch h.action {
	case hitFilter:
		if h.index < 0 || h.index >= len(m.filters) || h.index == m.filter {
			return m, nil
		}
		m.filter = h.index
		return m.refresh(false)
	case hitRow:
		if h.index == m.index {
			return m.openSelection()
		}
		return m.moveTo(h.index)
	case hitBack:
		if m.scope != nil {
			return m.leaveScope()
		}
	}
	return m, nil
}

func (m model) hitTest(x, y int) hit {
	if m.detail {
		return hit{action: hitPreview}
	}
	if y == m.searchY() && m.scope != nil {
		w := ansi.StringWidth(m.scope.Key)
		if x >= 1 && x < 1+w {
			return hit{action: hitBack}
		}
	}
	if y >= m.filterTop() && y < m.filterTop()+m.filterLineCount() {
		line := y - m.filterTop()
		for _, c := range m.chips {
			if c.line == line && x >= c.x0 && x < c.x1 {
				return hit{action: hitFilter, index: c.index}
			}
		}
		return hit{}
	}
	bodyTop := m.bodyTop()
	if y >= bodyTop && y < bodyTop+m.bodyHeight() {
		if m.wide() && x >= m.listWidth() {
			return hit{action: hitPreview}
		}
		if len(m.rows) == 0 {
			return hit{}
		}
		h := m.bodyHeight()
		start := max(0, m.index-h/2)
		start = max(0, min(start, len(m.rows)-h))
		row := start + (y - bodyTop)
		if row >= 0 && row < len(m.rows) {
			return hit{action: hitRow, index: row}
		}
	}
	return hit{}
}

// ----- view -----------------------------------------------------------------

// clip guarantees no line is wider than the terminal, whatever it holds.
func clip(s string, width, height int) string {
	lines := strings.Split(s, "\n")
	if height > 0 && len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		if ansi.StringWidth(line) > width {
			lines[i] = ansi.Truncate(line, width, "…")
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) View() tea.View {
	v := tea.NewView(clip(m.view(), m.width, m.height))
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m model) view() string {
	var b strings.Builder
	title := terminal.StyleAccent.Render("toyota") + "  " + terminal.StyleDim.Render(m.region)
	b.WriteString(" " + title + "\n\n")
	if m.detail {
		b.WriteString(m.viewport.View() + "\n")
		b.WriteString("\n" + terminal.StyleDim.Render(fmt.Sprintf("  %3.0f%%   ↑↓ pgup pgdn scroll   esc back", m.viewport.ScrollPercent()*100)))
		return b.String()
	}
	prompt := ""
	if m.scope != nil {
		prompt = terminal.StyleAccent.Render(m.scope.Key) + terminal.StyleDim.Render(" › ")
	}
	b.WriteString(" " + prompt + m.input.View() + "\n")
	b.WriteString(m.tabs() + "\n\n")
	b.WriteString(m.body())
	b.WriteString("\n" + m.hints())
	return b.String()
}

func (m model) tabs() string {
	if len(m.chips) == 0 {
		return " "
	}
	n := m.filterLineCount()
	lines := make([]string, n)
	for i := 0; i < n; i++ {
		var line strings.Builder
		col := 0
		for _, c := range m.chips {
			if c.line != i {
				continue
			}
			if c.x0 > col {
				line.WriteString(strings.Repeat(" ", c.x0-col))
			}
			if c.index == m.filter {
				line.WriteString(terminal.StyleAccent.Underline(true).Render(c.label))
			} else {
				line.WriteString(terminal.StyleDim.Render(c.label))
			}
			col = c.x1
		}
		lines[i] = line.String()
	}
	if status := m.status(); status != "" {
		last := n - 1
		room := m.width - ansi.StringWidth(lines[last]) - ansi.StringWidth(status) - 2
		if room >= 2 {
			lines[last] += strings.Repeat(" ", room) + terminal.StyleDim.Render(status)
		}
	}
	return strings.Join(lines, "\n")
}

func (m model) status() string {
	switch {
	case m.err != nil:
		return ""
	case m.loading && len(m.rows) == 0:
		return "searching…"
	case len(m.rows) == 0:
		return ""
	}
	n := fmt.Sprintf("%d", len(m.rows))
	if len(m.rows) >= resultLimit {
		n = fmt.Sprintf("first %d", resultLimit)
	}
	kind := ""
	if m.filter >= 0 && m.filter < len(m.filters) {
		kind = m.filters[m.filter].kind
	}
	switch {
	case m.query() == "" && m.scope == nil && (kind == "" || kind == "ecu"):
		return n + " ECUs with decoded catalogs"
	case m.query() == "" && m.scope == nil && kind == "vehicle":
		return n + " models"
	case m.query() == "":
		return n + " records"
	}
	return n + " results"
}

func (m model) hints() string {
	leave := "esc quit"
	switch {
	case m.query() != "":
		leave = "esc clear"
	case m.scope != nil:
		leave = "esc back"
	}
	for _, text := range []string{
		" ↑↓ move   ←→ filter   tab filter   enter open   click select   ctrl+r region   " + leave,
		" ↑↓ move   ←→ filter   tab filter   enter open   ctrl+r region   " + leave,
		" ↑↓ move   ←→ filter   tab filter   enter open   " + leave,
		" ↑↓   ←→   tab   enter   " + leave,
	} {
		if ansi.StringWidth(text) <= m.width {
			return terminal.StyleDim.Render(text)
		}
	}
	return terminal.StyleDim.Render(" " + leave)
}

func (m model) body() string {
	h := m.bodyHeight()
	left := m.listLines(h)
	if !m.wide() {
		return strings.Join(left, "\n") + "\n"
	}
	right := m.previewLines(h)
	gutter := terminal.StyleDim.Render(" │ ")
	lw := m.listWidth()
	var b strings.Builder
	for i := 0; i < h; i++ {
		b.WriteString(padTo(left[i], lw) + gutter + right[i] + "\n")
	}
	return b.String()
}

func padTo(s string, w int) string {
	if n := w - ansi.StringWidth(s); n > 0 {
		return s + strings.Repeat(" ", n)
	}
	return s
}

func (m model) emptyMessage() string {
	query := m.query()
	kind, label := "", ""
	if m.filter >= 0 && m.filter < len(m.filters) {
		kind, label = m.filters[m.filter].kind, m.filters[m.filter].label
	}
	switch {
	case m.err != nil:
		return terminal.StyleError.Render(m.err.Error())
	case m.loading:
		return terminal.StyleDim.Render("Searching…")
	case query != "":
		msg := "Nothing matches “" + query + "”."
		if m.scope != nil {
			msg += " Backspace leaves " + m.scope.Key + " to search everywhere."
		}
		return terminal.StyleDim.Render(msg)
	case m.scope != nil && kind != "":
		return terminal.StyleDim.Render("No " + label + " records in " + m.scope.Key + ".")
	case m.scope != nil:
		return terminal.StyleDim.Render("No records in " + m.scope.Key + ".")
	case kind != "" && kind != "ecu":
		return terminal.StyleDim.Render("No " + label + " records in " + m.region + ".")
	}
	return terminal.StyleDim.Render("No ECUs with decoded catalogs in " + m.region + ".")
}

func (m model) listLines(h int) []string {
	lines := make([]string, h)
	if len(m.rows) == 0 {
		lines[0] = "  " + m.emptyMessage()
		return lines
	}
	start := max(0, m.index-h/2)
	start = max(0, min(start, len(m.rows)-h))
	terms := terminal.Terms(m.query())
	// The kind column only earns its width when kinds are mixed.
	mixed := m.filter == 0 && (m.scope != nil || m.query() != "")
	for i := 0; i < h && start+i < len(m.rows); i++ {
		lines[i] = m.row(m.rows[start+i], start+i == m.index, terms, mixed)
	}
	return lines
}

// row lays out: marker kind ecu identifier name detail.
func (m model) row(it catalog.Item, selected bool, terms []string, showKind bool) string {
	w := m.listWidth()
	st := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(terminal.SelectedBG)
		}
		return s
	}
	cell := func(text string, style lipgloss.Style, cw int) string {
		text = ansi.Truncate(strings.Join(strings.Fields(text), " "), cw, "…")
		return st(style).Render(padTo(text, cw))
	}
	gap := st(lipgloss.NewStyle()).Render("  ")
	marker := "  "
	if selected {
		marker = "▸ "
	}
	var b strings.Builder
	b.WriteString(st(terminal.StyleAccent).Render(marker))
	used := 2
	if showKind && w >= 40 {
		b.WriteString(cell(terminal.KindLabel(it.Kind), terminal.KindStyle(it.Kind), 7) + gap)
		used += 9
	}
	if m.scope == nil && w >= 56 {
		owner, ownerW := it.ECU, 12
		if owner == "" {
			owner = "—"
		}
		if it.Kind == "ecu" {
			ownerW = 17
		}
		b.WriteString(cell(owner, terminal.StyleBlue, ownerW) + gap)
		used += ownerW + 2
	}
	if w >= 30 {
		identW := 9
		b.WriteString(cell(it.Identifier, terminal.StyleDim, identW) + gap)
		used += identW + 2
	}
	room := max(4, w-used)
	b.WriteString(m.nameCell(it, room, terms, selected))
	return b.String()
}

func (m model) nameCell(it catalog.Item, room int, terms []string, selected bool) string {
	name := strings.Join(strings.Fields(it.Name), " ")
	detail := strings.Join(strings.Fields(it.Detail), " ")
	if it.Kind == "ecu" && m.wide() {
		detail = "" // the preview already carries the counts
	}
	if ansi.StringWidth(name) > room {
		name = ansi.Truncate(name, room, "…")
		detail = ""
	}
	left := room - ansi.StringWidth(name) - 2
	if left < 6 {
		detail = ""
	} else if detail != "" {
		detail = ansi.Truncate(detail, left, "…")
	}
	used := ansi.StringWidth(name)
	if selected {
		bg := lipgloss.NewStyle().Background(terminal.SelectedBG)
		out := bg.Bold(true).Render(name)
		if detail != "" {
			out += bg.Render("  ") + terminal.StyleDim.Background(terminal.SelectedBG).Render(detail)
			used += 2 + ansi.StringWidth(detail)
		}
		return out + bg.Render(strings.Repeat(" ", max(0, room-used)))
	}
	out := terminal.Mark(name, terms)
	if detail != "" {
		out += "  " + terminal.StyleDim.Render(detail)
	}
	return out
}

func (m model) previewLines(h int) []string {
	lines := make([]string, h)
	w := m.previewWidth()
	if m.preview == "" {
		if len(m.rows) > 0 {
			lines[0] = terminal.StyleDim.Render("Loading…")
		}
		return lines
	}
	all := strings.Split(strings.TrimRight(m.preview, "\n"), "\n")
	top := max(0, min(m.previewTop, max(0, len(all)-h)))
	for i := 0; i < h && top+i < len(all); i++ {
		lines[i] = ansi.Truncate(all[top+i], w, "…")
	}
	return lines
}

// Run opens the explorer on in/out.
func Run(db *catalog.DB, region string, in io.Reader, out io.Writer) error {
	p := tea.NewProgram(newModel(db, region), tea.WithInput(in), tea.WithOutput(out))
	_, err := p.Run()
	return err
}
