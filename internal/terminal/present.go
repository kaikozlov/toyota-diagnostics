// Package terminal renders compact, readable diagnostics without changing JSON output.
package terminal

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

type Printer struct {
	Out   io.Writer
	Width int
	Color bool
}

func New(out io.Writer) Printer {
	color := os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	width := 100
	if f, ok := out.(*os.File); ok {
		color = color && term.IsTerminal(int(f.Fd()))
		if w, _, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			width = w
		}
	} else {
		color = false
	}
	return Printer{Out: out, Width: width, Color: color}
}

func (p Printer) style(s string, color lipgloss.Color, bold bool) string {
	if !p.Color {
		return s
	}
	return lipgloss.NewStyle().Foreground(color).Bold(bold).Render(s)
}

var (
	accent    = lipgloss.Color("#72D8B1")
	muted     = lipgloss.Color("#89939C")
	strong    = lipgloss.Color("#DFE7ED")
	secondary = lipgloss.Color("#94B5FF")
)

func cut(s string, width int) string {
	s = strings.ReplaceAll(strings.ReplaceAll(s, "\r", " "), "\n", " ")
	if width < 2 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	// Rune-safe fallback; terminal widths are measured with lipgloss.Width.
	var b strings.Builder
	for _, r := range s {
		next := b.String() + string(r)
		if lipgloss.Width(next) >= width {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func pad(s string, width int) string {
	n := width - lipgloss.Width(s)
	if n < 0 {
		return cut(s, width)
	}
	return s + strings.Repeat(" ", n)
}

func (p Printer) heading(title, meta string) {
	fmt.Fprintln(p.Out, p.style(title, accent, true)+"  "+p.style(meta, muted, false))
}

func (p Printer) Search(query, region string, rows []catalog.Item) {
	p.heading("SEARCH", fmt.Sprintf("%q   %s · %d shown", query, region, len(rows)))
	fmt.Fprintln(p.Out)
	if len(rows) == 0 {
		fmt.Fprintln(p.Out, "  No matches")
		return
	}
	kindWidth, ecuWidth, idWidth := 13, 17, 12
	textWidth := p.Width - (kindWidth + ecuWidth + idWidth + 8)
	if textWidth < 24 {
		textWidth = 24
	}
	for _, r := range rows {
		owner := r.ECU
		if r.Kind == "vehicle" {
			owner = "vehicle"
		}
		if owner == "" {
			owner = "general"
		}
		line := fmt.Sprintf("  %s %s %s %s",
			pad(p.style(cut(r.Kind, kindWidth), muted, false), kindWidth),
			pad(p.style(cut(owner, ecuWidth), secondary, false), ecuWidth),
			pad(p.style(cut(r.Identifier, idWidth), muted, false), idWidth),
			p.style(cut(r.Name, textWidth), strong, false))
		fmt.Fprintln(p.Out, line)
	}
}

func (p Printer) ECU(e catalog.ECU) {
	p.heading(strings.ToUpper(e.Key), e.Name)
	fmt.Fprintln(p.Out)
	fmt.Fprintf(p.Out, "  %-15s %d  ·  %s\n", "Category", e.CategoryID, e.Region)
	if e.Database != "" {
		fmt.Fprintf(p.Out, "  %-15s %s\n", "GTS database", e.Database)
	}
	if e.Generation != nil {
		fmt.Fprintf(p.Out, "  %-15s %d\n", "Generation", *e.Generation)
	}
	if e.SupportMode != "" {
		fmt.Fprintf(p.Out, "  %-15s %s\n", "Support", e.SupportMode)
	}
	if e.CatalogMember == "" {
		fmt.Fprintln(p.Out, "  Catalog         No decoded ECU catalog")
		return
	}
	fmt.Fprintln(p.Out)
	for _, row := range []struct{ key, label string }{
		{"dids", "Data IDs"}, {"signals", "Signals"}, {"dtcs", "DTCs"},
		{"active_tests", "Active Tests"}, {"functions", "Functions"},
		{"plugins", "Plugins"}, {"utilities", "Utilities"},
	} {
		fmt.Fprintf(p.Out, "  %-15s %s\n", row.label, strconv.Itoa(e.Counts[row.key]))
	}
	fmt.Fprintln(p.Out)
	fmt.Fprintln(p.Out, p.style("  toyota "+e.Key+" data     ·     toyota "+e.Key+" dtcs     ·     toyota "+e.Key+" active-tests", muted, false))
}

func (p Printer) ECUs(region string, rows []catalog.ECU) {
	p.heading("ECUs", fmt.Sprintf("%s · %d shown", region, len(rows)))
	fmt.Fprintln(p.Out)
	if len(rows) == 0 {
		fmt.Fprintln(p.Out, "  No matching ECUs")
		return
	}
	for _, e := range rows {
		labels := fmt.Sprintf("%d", e.CategoryID)
		if e.CatalogMember != "" {
			labels += "  ·  decoded"
		}
		fmt.Fprintf(p.Out, "  %s  %s  %s\n", pad(p.style(cut(e.Key, 21), secondary, false), 21),
			pad(cut(e.Name, 38), 38), p.style(labels, muted, false))
	}
}

func (p Printer) Items(e catalog.ECU, kind string, rows []catalog.Item) {
	label := map[string]string{"did": "DATA LIST", "dtc": "DTCS", "active-test": "ACTIVE TESTS",
		"function": "FUNCTIONS", "plugin": "PLUGINS", "utility": "UTILITIES"}[kind]
	if label == "" {
		label = strings.ToUpper(kind)
	}
	p.heading(label, fmt.Sprintf("%s · %s · %d shown", e.Key, e.Name, len(rows)))
	fmt.Fprintln(p.Out)
	if len(rows) == 0 {
		fmt.Fprintln(p.Out, "  No matches")
		return
	}
	idWidth := 12
	for _, row := range rows {
		max := p.Width - (idWidth + 5)
		if max < 24 {
			max = 24
		}
		fmt.Fprintf(p.Out, "  %s  %s\n",
			pad(p.style(cut(row.Identifier, idWidth), secondary, false), idWidth),
			cut(row.Name, max))
	}
}

func (p Printer) Vehicles(region string, rows []catalog.Vehicle) {
	p.heading("VEHICLES", fmt.Sprintf("%s · %d shown", region, len(rows)))
	fmt.Fprintln(p.Out)
	for _, v := range rows {
		fmt.Fprintf(p.Out, "  %s  %s\n", pad(p.style(strconv.Itoa(v.Type), secondary, false), 8), v.Name)
	}
}

func (p Printer) Vehicle(v catalog.Vehicle, candidates []catalog.VehicleECU) {
	p.heading("VEHICLE", v.Name)
	fmt.Fprintln(p.Out)
	fmt.Fprintf(p.Out, "  Toyota type     %d\n  Region          %s\n", v.Type, v.Region)
	sets := make([]string, len(v.InstallSetIDs))
	for i, set := range v.InstallSetIDs {
		sets[i] = strconv.Itoa(set)
	}
	fmt.Fprintf(p.Out, "  Install sets    %s\n", strings.Join(sets, " · "))
	fmt.Fprintln(p.Out)
	p.heading("INSTALL-SET ECUS", fmt.Sprintf("%d candidates", len(candidates)))
	fmt.Fprintln(p.Out)
	if len(candidates) == 0 {
		fmt.Fprintln(p.Out, "  No indexed install-set rows")
		return
	}
	for _, e := range candidates {
		endpoint := "—"
		if e.RequestAddress != nil {
			endpoint = fmt.Sprintf("0x%X", *e.RequestAddress)
			if e.AddressExtension != nil {
				endpoint += fmt.Sprintf("/0x%02X", *e.AddressExtension)
			}
		}
		fmt.Fprintf(p.Out, "  %s  %s  %s  %s\n",
			pad(p.style(cut(e.Key, 19), secondary, false), 19),
			pad(cut(e.Name, 33), 33),
			pad(p.style(endpoint, muted, false), 12),
			p.style(cut(e.TransportKind, 30), muted, false))
	}
}

func (p Printer) Stats(dbPath string, s catalog.Stats) {
	p.heading("CATALOG", s.Release)
	fmt.Fprintln(p.Out)
	fmt.Fprintf(p.Out, "  File            %s\n  Schema          %s\n  Source          %s\n", dbPath, s.Schema, s.SourceSchema)
	fmt.Fprintf(p.Out, "  Source members  %d\n  Search facts    %d\n  Vehicles        %d\n  ECU categories  %d\n",
		s.Members, s.Items, s.Vehicles, s.ECUs)
}

func (p Printer) Help() {
	fmt.Fprintln(p.Out, p.style("TOYOTA", accent, true)+"  "+p.style("DIAGNOSTICS", strong, true))
	fmt.Fprintln(p.Out, p.style("  GTS+ discovery and vehicle diagnostics", muted, false))
	fmt.Fprintln(p.Out)
	for _, row := range []struct{ cmd, detail string }{
		{"toyota", "Interactive explorer (TTY)"},
		{"toyota search LTA", "Search Toyota's OEM terminology"},
		{"toyota frc", "Inspect an ECU by name or category ID"},
		{"toyota frc data steer", "Browse OEM Data List signals"},
		{"toyota frc active-tests", "Browse available Active Tests"},
		{"toyota vehicle Camry", "Find Toyota vehicle identities"},
		{"toyota db status", "Inspect the complete SQLite catalog"},
		{"toyota monitor frc LTA", "Live decoded Data List monitor"},
	} {
		fmt.Fprintf(p.Out, "  %s  %s\n", pad(p.style(row.cmd, secondary, false), 33), row.detail)
	}
	fmt.Fprintln(p.Out)
	fmt.Fprintln(p.Out, p.style("  --region NA|EU|JP    --db PATH    --json    --limit N", muted, false))
}

func (p Printer) Error(message string) {
	if !utf8.ValidString(message) {
		message = strings.ToValidUTF8(message, "�")
	}
	fmt.Fprintln(p.Out, p.style("error: ", lipgloss.Color("#ED8C8C"), true)+message)
}
