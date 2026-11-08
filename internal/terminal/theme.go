package terminal

import (
	"image/color"
	"os"
	"slices"
	"strconv"
	"strings"

	"charm.land/lipgloss/v2"
	"golang.org/x/term"

	"github.com/kaikozlov/toyota-diagnostics/internal/appenv"
)

// The palette is resolved once. Terminal background detection is deliberately
// environment-based: asking the terminal (OSC 11) can stall for seconds on
// terminals that never answer, and a diagnostics tool must start instantly.
type palette struct {
	accent, dim, blue, red, amber, violet, cyan, selected color.Color
}

var theme = resolvePalette()

func resolvePalette() palette {
	if lightBackground() {
		return palette{
			accent: lipgloss.Color("#0B7A5A"), dim: lipgloss.Color("#66727C"), blue: lipgloss.Color("#2C55C9"), red: lipgloss.Color("#B3261E"),
			amber: lipgloss.Color("#8A5A00"), violet: lipgloss.Color("#7B3FC4"), cyan: lipgloss.Color("#00758A"), selected: lipgloss.Color("#D5EBE3"),
		}
	}
	return palette{
		accent: lipgloss.Color("#72D8B1"), dim: lipgloss.Color("#7D8A95"), blue: lipgloss.Color("#8FB0FF"), red: lipgloss.Color("#F28B82"),
		amber: lipgloss.Color("#E8C170"), violet: lipgloss.Color("#D2A8FF"), cyan: lipgloss.Color("#6FD3E6"), selected: lipgloss.Color("#25444D"),
	}
}

// lightBackground honours TOYO_THEME (or TOYOTA_THEME), then the COLORFGBG convention
// ("fg;bg", where bg 7 or 15 is a light terminal). Dark is the default.
func lightBackground() bool {
	switch strings.ToLower(appenv.Theme()) {
	case "light":
		return true
	case "dark":
		return false
	}
	if parts := strings.Split(os.Getenv("COLORFGBG"), ";"); len(parts) >= 2 {
		if bg, err := strconv.Atoi(parts[len(parts)-1]); err == nil {
			return bg == 7 || bg == 15
		}
	}
	return false
}

// Colour reports whether styled output should be produced for f, honouring
// NO_COLOR, TERM=dumb and CLICOLOR_FORCE. Profile downsampling happens in the
// writer (colorprofile), not a global renderer setting.
func Colour(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" || os.Getenv("TERM") == "dumb" {
		return false
	}
	if force := os.Getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
		return true
	}
	return term.IsTerminal(int(f.Fd()))
}

// colorEnv is the environment colour detection sees. Forced colour on a dumb
// or unset TERM keeps working by claiming a 256-colour terminal.
func colorEnv() []string {
	env := os.Environ()
	if force := os.Getenv("CLICOLOR_FORCE"); force != "" && force != "0" {
		if t := os.Getenv("TERM"); t == "" || t == "dumb" {
			env = append(slices.Clone(env), "TERM=xterm-256color")
		}
	}
	return env
}

// Kind is how a catalog record type is named and coloured everywhere.
type Kind struct {
	Label string // short column label
	Noun  string // sentence form
	color color.Color
}

func kindInfo(kind string) Kind {
	switch kind {
	case "did":
		return Kind{"data", "Data List signal", theme.blue}
	case "dtc":
		return Kind{"dtc", "Trouble code", theme.red}
	case "active-test":
		return Kind{"test", "Active Test", theme.violet}
	case "adu-signal":
		return Kind{"adu", "ADAS recorder signal", theme.cyan}
	case "ffd-signal":
		return Kind{"ffd", "Freeze-frame signal", theme.cyan}
	case "ffd-rob":
		return Kind{"rob", "Freeze-frame trigger", theme.cyan}
	case "ecu":
		return Kind{"ecu", "ECU", theme.accent}
	case "vehicle":
		return Kind{"vehicle", "Vehicle", theme.amber}
	case "plugin":
		return Kind{"plugin", "Plugin binding", theme.dim}
	case "function":
		return Kind{"func", "Function", theme.dim}
	case "utility":
		return Kind{"utility", "Utility", theme.amber}
	case "utility-family":
		return Kind{"family", "Utility family", theme.amber}
	case "data-list":
		return Kind{"list", "Data List", theme.blue}
	case "generic-ffd-signal":
		return Kind{"ffd", "Generic freeze-frame signal", theme.cyan}
	case "rob-code", "rob-signal":
		return Kind{"rob", "Record of Behavior", theme.cyan}
	case "command", "selector":
		return Kind{kind, strings.ToUpper(kind[:1]) + kind[1:], theme.dim}
	case "active-test-group":
		return Kind{"group", "Active Test group", theme.violet}
	case "customize-group", "customize-item", "customize-probe":
		return Kind{"custom", "Customize " + strings.TrimPrefix(kind, "customize-"), theme.amber}
	case "legacy-pid", "legacy-did", "legacy-freeze-frame", "legacy-active-test":
		return Kind{"legacy", "Legacy " + strings.ReplaceAll(strings.TrimPrefix(kind, "legacy-"), "-", " "), theme.dim}
	}
	return Kind{kind, kind, theme.dim}
}

// KindLabel is the short display name of a catalog kind.
func KindLabel(kind string) string { return kindInfo(kind).Label }

// KindNoun is the sentence-form name of a catalog kind.
func KindNoun(kind string) string { return kindInfo(kind).Noun }

// SelectedBG is the background of the highlighted row.
var SelectedBG color.Color = theme.selected

// Styles shared with the interactive explorer, which renders through Bubble
// Tea rather than a Printer.
var (
	StyleAccent   = lipgloss.NewStyle().Foreground(theme.accent).Bold(true)
	StyleDim      = lipgloss.NewStyle().Foreground(theme.dim)
	StyleBlue     = lipgloss.NewStyle().Foreground(theme.blue)
	StyleError    = lipgloss.NewStyle().Foreground(theme.red).Bold(true)
	StyleSelected = lipgloss.NewStyle().Background(theme.selected).Bold(true)
)

// KindStyle styles a catalog kind label.
func KindStyle(kind string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(kindInfo(kind).color)
}
