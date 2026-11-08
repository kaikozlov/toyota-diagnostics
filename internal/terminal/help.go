package terminal

import "strings"

// HelpEntry is one line of a help table: what to type, and what it does.
type HelpEntry struct{ Usage, Summary string }

// HelpGroup is a titled block of entries.
type HelpGroup struct {
	Title   string
	Entries []HelpEntry
}

// Overview prints the front-door help screen.
func (p Printer) Overview(tagline string, groups []HelpGroup, notes []string) {
	p.println(p.accent("toyota") + "  " + p.dim(tagline))
	var all []HelpEntry
	for _, g := range groups {
		all = append(all, g.Entries...)
	}
	w := usageWidth(all)
	for _, g := range groups {
		p.println()
		p.section(g.Title, "")
		p.table(g.Entries, w)
	}
	if len(notes) > 0 {
		p.println()
		for _, n := range notes {
			p.println("  " + p.dim(n))
		}
	}
}

// CommandHelp prints focused help for one command.
func (p Printer) CommandHelp(name, summary string, usage []string, examples, options []HelpEntry) {
	p.println(p.accent("toyota "+name) + "  " + p.dim(summary))
	p.println()
	for _, u := range usage {
		p.println("  " + p.highlightUsage(u))
	}
	if len(examples) > 0 {
		p.println()
		p.section("Examples", "")
		p.table(examples, usageWidth(append(append([]HelpEntry(nil), examples...), options...)))
	}
	if len(options) > 0 {
		p.println()
		p.section("Options", "")
		p.table(options, usageWidth(append(append([]HelpEntry(nil), examples...), options...)))
	}
}

func usageWidth(entries []HelpEntry) int {
	w := 0
	for _, e := range entries {
		w = max(w, width(e.Usage))
	}
	return min(w, 38)
}

func (p Printer) table(entries []HelpEntry, w int) {
	for _, e := range entries {
		use := p.highlightUsage(e.Usage)
		if width(e.Usage) > w {
			p.println("  " + use)
			p.println("  " + strings.Repeat(" ", w+2) + p.dim(e.Summary))
			continue
		}
		p.println(strings.TrimRight("  "+pad(use, w+2)+p.dim(e.Summary), " "))
	}
}

// highlightUsage quiets placeholders (UPPERCASE words and [optional] parts)
// so the literal command stands out.
func (p Printer) highlightUsage(u string) string {
	if !p.Color {
		return u
	}
	words := strings.Split(u, " ")
	for i, w := range words {
		bare := strings.Trim(w, "[]|.…")
		switch {
		case i == 0 && w == "toyota":
		case w != "" && (bare == strings.ToUpper(bare) && strings.ContainsAny(bare, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") || strings.HasPrefix(w, "[") || strings.HasPrefix(w, "--")):
			words[i] = p.dim(w)
		default:
			words[i] = p.blue(w)
		}
	}
	return strings.Join(words, " ")
}
