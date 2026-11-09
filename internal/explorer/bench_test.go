package explorer

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

// drain feeds each command's message back until the model is idle, skipping
// the debounce timer and cursor blink.
func drain(m model, cmd tea.Cmd) model {
	for cmd != nil {
		msg := cmd()
		if _, batch := msg.(tea.BatchMsg); batch || msg == nil {
			break
		}
		m, cmd = update(m, msg)
	}
	return m
}

func ready(d *catalog.DB) model {
	m := newModel(d, "NA")
	m, _ = update(m, tea.WindowSizeMsg{Width: 120, Height: 40})
	m.revision++
	return drain(m, m.load())
}

// BenchmarkExplorer measures startup, a full render, and the work one
// keystroke triggers against the shipped catalog.
func BenchmarkExplorer(b *testing.B) {
	d, err := catalog.Open("../../catalog/catalog.sqlite")
	if errors.Is(err, catalog.ErrLFSPointer) {
		b.Skip("catalog/catalog.sqlite is an LFS pointer; run git lfs pull")
	}
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { d.Close() })
	b.Run("startup", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			view(ready(d))
		}
	})
	b.Run("render", func(b *testing.B) {
		m := ready(d)
		b.ReportAllocs()
		for b.Loop() {
			view(m)
		}
	})
	for _, query := range []string{"s", "steer", "0x1601", "P0171", "engine"} {
		b.Run("key/"+query, func(b *testing.B) {
			m := ready(d)
			b.ReportAllocs()
			for b.Loop() {
				next := m
				next.input.SetValue(query)
				next.revision++
				view(drain(next, next.load()))
			}
		})
	}
}
