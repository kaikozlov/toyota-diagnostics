// Package explorer provides a keyboard-first, indexed Toyota catalog browser.
package explorer

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

var (
	header   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#72D8B1"))
	dim      = lipgloss.NewStyle().Foreground(lipgloss.Color("#8496A2"))
	label    = lipgloss.NewStyle().Foreground(lipgloss.Color("#9DBAFF"))
	selected = lipgloss.NewStyle().Background(lipgloss.Color("#294D5A")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true)
)

type model struct {
	db      *catalog.DB
	region  string
	release string
	input   textinput.Model
	rows    []catalog.Item
	index   int
	width   int
	height  int
	detail  bool
	err     error
}

func newModel(db *catalog.DB, region, release string) model {
	input := textinput.New()
	input.Placeholder = "Signals, ECUs, DTCs, vehicles, Active Tests…"
	input.Prompt = "  / "
	input.CharLimit = 180
	input.Width = 70
	input.PromptStyle = header
	input.TextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#EFF6FA"))
	input.Focus()
	return model{db: db, region: region, release: release, input: input, width: 100, height: 28}
}

func (m model) Init() tea.Cmd { return textinput.Blink }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.width < 40 {
			m.width = 40
		}
		m.input.Width = m.width - 8
		if m.input.Width > 95 {
			m.input.Width = 95
		}
	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.detail {
				m.detail = false
				m.input.Focus()
				return m, textinput.Blink
			}
			return m, tea.Quit
		case "up", "ctrl+p":
			if m.detail {
				return m, nil
			}
			if m.index > 0 {
				m.index--
			}
			return m, nil
		case "down", "ctrl+n":
			if m.detail {
				return m, nil
			}
			if m.index < len(m.rows)-1 {
				m.index++
			}
			return m, nil
		case "enter":
			if m.detail {
				m.detail = false
				m.input.Focus()
				return m, textinput.Blink
			}
			if len(m.rows) > 0 {
				m.detail = true
				m.input.Blur()
			}
			return m, nil
		}
		if m.detail {
			return m, nil
		}
	}
	previous := m.input.Value()
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if m.input.Value() != previous {
		m.index = 0
		m.err = nil
		m.rows, m.err = m.db.Search(context.Background(), m.region, strings.TrimSpace(m.input.Value()), 200)
	}
	return m, cmd
}

func trim(s string, width int) string {
	s = strings.Join(strings.Fields(s), " ")
	if width <= 2 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	var b strings.Builder
	for _, r := range s {
		if lipgloss.Width(b.String()+string(r)+"…") > width {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString("\n  " + header.Render("TOYOTA / DIAGNOSTICS") + "  " + dim.Render(m.region+" · GTS+ "+m.release) + "\n\n")
	if m.detail {
		return m.detailView(b.String())
	}

	b.WriteString(m.input.View() + "\n\n")
	if m.err != nil {
		b.WriteString("  " + m.err.Error() + "\n")
	} else if strings.TrimSpace(m.input.Value()) == "" {
		b.WriteString(dim.Render("  VEHICLES      ECUs      DATA LIST      DTCs      ACTIVE TESTS      RECORDER FIELDS") + "\n\n")
		b.WriteString(dim.Render("  Type to search the complete Toyota catalog.") + "\n")
	} else {
		b.WriteString(dim.Render(fmt.Sprintf("  %d shown   ·   %s", len(m.rows), m.region)) + "\n\n")
		visible := m.height - 11
		if visible < 4 {
			visible = 4
		}
		start := m.index - visible/2
		if start < 0 {
			start = 0
		}
		if len(m.rows) > visible && start > len(m.rows)-visible {
			start = len(m.rows) - visible
		}
		for i := start; i < len(m.rows) && i < start+visible; i++ {
			r := m.rows[i]
			owner := r.ECU
			if owner == "" {
				owner = r.Region
			}
			left := fmt.Sprintf(" %-12s %-13s %-10s", trim(r.Kind, 12), trim(owner, 13), trim(r.Identifier, 10))
			name := trim(r.Name, m.width-43)
			if i == m.index {
				b.WriteString(selected.Render(trim("  ▸"+left+" "+name, m.width-2)) + "\n")
			} else {
				b.WriteString(dim.Render("  "+left) + " " + label.Render(name) + "\n")
			}
		}
	}
	b.WriteString("\n" + dim.Render("  ↑↓ navigate     enter inspect     esc exit") + "\n")
	return b.String()
}

func (m model) detailView(prefix string) string {
	if len(m.rows) == 0 {
		return prefix
	}
	r := m.rows[m.index]
	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString("  " + header.Render(trim(r.Name, m.width-4)) + "\n\n")
	for _, field := range [][2]string{
		{"Kind", r.Kind},
		{"ECU", r.ECUName},
		{"Category", fmt.Sprintf("%d", r.CategoryID)},
		{"Identifier", r.Identifier},
		{"Region", r.Region},
		{"Detail", r.Detail},
	} {
		if field[1] == "" {
			continue
		}
		b.WriteString(dim.Render(fmt.Sprintf("  %-13s ", field[0])) + trim(field[1], m.width-21) + "\n")
	}
	if r.ECU != "" {
		b.WriteString("\n" + dim.Render(fmt.Sprintf("  toyota %s    ·    toyota %s data", r.ECU, r.ECU)) + "\n")
	}
	b.WriteString("\n" + dim.Render("  enter / esc back     ctrl+c exit") + "\n")
	return b.String()
}

func Run(db *catalog.DB, region string, in io.Reader, out io.Writer) error {
	release, err := db.Meta(context.Background(), "release")
	if err != nil {
		return err
	}
	p := tea.NewProgram(newModel(db, region, release),
		tea.WithAltScreen(), tea.WithInput(in), tea.WithOutput(out))
	_, err = p.Run()
	return err
}
