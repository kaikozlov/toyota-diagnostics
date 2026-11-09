// Package terminal renders catalog records for people: compact, aligned,
// quiet by default, and identical in shape whether piped or on a TTY.
package terminal

import (
	"bytes"
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"golang.org/x/term"
)

// maxMeasure keeps lines readable on very wide terminals.
const maxMeasure = 112

// Printer renders to Out. Width is the terminal width; Color gates all styling
// so piped output is plain text with the same alignment.
type Printer struct {
	Out    io.Writer
	Width  int
	Height int
	Color  bool
	// NoHints drops "what to type next" footers, for surfaces (the explorer)
	// that already show their own keys.
	NoHints bool
}

// New sizes a Printer to out when it is a terminal. Styled output is
// downsampled to the terminal's colour profile by the writer.
func New(out io.Writer) Printer {
	p := Printer{Out: out, Width: 100, Height: 40}
	if f, ok := out.(*os.File); ok {
		p.Color = Colour(f)
		if w, h, err := term.GetSize(int(f.Fd())); err == nil && w > 0 {
			p.Width, p.Height = w, h
		}
	}
	if p.Color {
		p.Out = colorprofile.NewWriter(out, colorEnv())
	}
	return p
}

// Render runs fn against an in-memory Printer and returns what it wrote. The
// explorer uses it to show exactly the views the command line prints.
func Render(width int, color bool, fn func(Printer)) string {
	var buf bytes.Buffer
	fn(Printer{Out: &buf, Width: width, Height: 40, Color: color, NoHints: true})
	return buf.String()
}

// measure is the usable line width.
func (p Printer) measure() int { return max(40, min(p.Width, maxMeasure)) }

func (p Printer) narrow() bool { return p.Width < 72 }

func (p Printer) paint(s string, color color.Color, bold bool) string {
	if !p.Color || s == "" {
		return s
	}
	return lipgloss.NewStyle().Foreground(color).Bold(bold).Render(s)
}

func (p Printer) bold(s string) string {
	if !p.Color || s == "" {
		return s
	}
	return lipgloss.NewStyle().Bold(true).Render(s)
}

func (p Printer) accent(s string) string { return p.paint(s, theme.accent, true) }
func (p Printer) dim(s string) string    { return p.paint(s, theme.dim, false) }
func (p Printer) blue(s string) string   { return p.paint(s, theme.blue, false) }
func (p Printer) kind(k string) string   { return p.paint(KindLabel(k), kindInfo(k).color, false) }

func (p Printer) printf(format string, args ...any) { fmt.Fprintf(p.Out, format, args...) }
func (p Printer) println(args ...any)               { fmt.Fprintln(p.Out, args...) }

func width(s string) int { return ansi.StringWidth(s) }

// oneLine flattens whitespace so a record can never break a row.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// fit shortens plain text to w cells.
func fit(s string, w int) string {
	s = oneLine(s)
	if w <= 0 {
		return ""
	}
	if width(s) <= w {
		return s
	}
	return ansi.Truncate(s, w, "…")
}

// pad right-pads styled text whose visible width is already known.
func pad(styled string, w int) string {
	if n := w - width(styled); n > 0 {
		return styled + strings.Repeat(" ", n)
	}
	return styled
}

func padLeft(styled string, w int) string {
	if n := w - width(styled); n > 0 {
		return strings.Repeat(" ", n) + styled
	}
	return styled
}

func colWidth(values []string, limit int) int {
	w := 0
	for _, v := range values {
		w = max(w, width(oneLine(v)))
	}
	return min(w, limit)
}

func (p Printer) mark(s string, terms []string) string {
	if !p.Color {
		return s
	}
	return Mark(s, terms)
}

// Mark emphasises query terms inside s. It only touches matches whose
// lower-cased form keeps byte offsets valid.
func Mark(s string, terms []string) string {
	if len(terms) == 0 || s == "" {
		return s
	}
	lower := strings.ToLower(s)
	if len(lower) != len(s) {
		return s
	}
	hit := make([]bool, len(s))
	any := false
	for _, t := range terms {
		t = strings.ToLower(t)
		if len(t) == 0 {
			continue
		}
		for from := 0; ; {
			i := strings.Index(lower[from:], t)
			if i < 0 {
				break
			}
			for k := from + i; k < from+i+len(t); k++ {
				hit[k] = true
			}
			any = true
			from += i + len(t)
		}
	}
	if !any {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && hit[j] == hit[i] {
			j++
		}
		if hit[i] {
			b.WriteString(lipgloss.NewStyle().Foreground(theme.accent).Bold(true).Render(s[i:j]))
		} else {
			b.WriteString(s[i:j])
		}
		i = j
	}
	return b.String()
}

// Terms splits a query the way a person would read it.
func Terms(query string) []string {
	return strings.FieldsFunc(query, func(r rune) bool {
		return !(r == '_' || r == '.' || r == '-' || r == '+' || r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' || r > 127)
	})
}

// nameDetail lays out a record name followed by a quieter detail, truncating
// the detail first.
func (p Printer) nameDetail(name, detail string, w int, terms []string) string {
	name, detail = oneLine(name), oneLine(detail)
	if width(name) >= w {
		return p.mark(fit(name, w), terms)
	}
	out := p.mark(name, terms)
	if room := w - width(name) - 2; detail != "" && room >= 6 {
		out += "  " + p.dim(fit(detail, room))
	}
	return out
}

// title prints the subject on the left and context flush right.
func (p Printer) title(left, right string) {
	if right == "" {
		p.println(left)
		return
	}
	gap := p.measure() - width(left) - width(right)
	if gap < 2 {
		p.println(left)
		if right != "" {
			p.println(p.dim(strings.TrimSpace(ansi.Strip(right))))
		}
		return
	}
	p.println(left + strings.Repeat(" ", gap) + right)
}

// section prints a quiet divider label.
func (p Printer) section(label, meta string) {
	line := p.bold(label)
	if meta != "" {
		line += "  " + p.dim(meta)
	}
	p.println(line)
}

const labelWidth = 14

// kv prints an aligned label and a value that wraps under itself.
func (p Printer) kv(label, value string) {
	if value == "" {
		return
	}
	room := p.measure() - 2 - labelWidth
	lines := strings.Split(ansi.Wordwrap(value, room, ""), "\n")
	for i, line := range lines {
		head := strings.Repeat(" ", labelWidth)
		if i == 0 {
			head = pad(p.dim(label), labelWidth)
		}
		p.println("  " + head + line)
	}
}

// footer prints quiet next-step hints, wrapping between hints rather than
// inside one.
func (p Printer) footer(parts ...string) {
	if p.NoHints {
		return
	}
	p.hintLines(parts...)
}

func (p Printer) hintLines(parts ...string) {
	const sep = "  ·  "
	var lines []string
	line := ""
	for _, s := range parts {
		if s == "" {
			continue
		}
		if line != "" && width(line)+len(sep)+width(s) > p.measure()-2 {
			lines = append(lines, line)
			line = ""
		}
		if line != "" {
			line += sep
		}
		line += s
	}
	if line != "" {
		lines = append(lines, line)
	}
	if len(lines) == 0 {
		return
	}
	p.println()
	for _, l := range lines {
		p.println(p.dim("  " + l))
	}
}

func count(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "," + s[i:]
	}
	return s
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", one)
	}
	return fmt.Sprintf("%s %s", count(n), many)
}
