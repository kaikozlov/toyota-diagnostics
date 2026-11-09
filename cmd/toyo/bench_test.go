package main

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

// BenchmarkCommand runs whole commands against the shipped catalog.
func BenchmarkCommand(b *testing.B) {
	const db = "../../catalog/catalog.sqlite"
	d, err := catalog.Open(db)
	if errors.Is(err, catalog.ErrLFSPointer) {
		b.Skip("catalog/catalog.sqlite is an LFS pointer; run git lfs pull")
	}
	if err != nil {
		b.Fatal(err)
	}
	d.Close()
	null, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		b.Fatal(err)
	}
	stdout := os.Stdout
	os.Stdout = null
	b.Cleanup(func() { os.Stdout = stdout; null.Close() })
	for _, command := range []string{
		"help",
		"search LTA",
		"search 0x1601",
		"search P0171",
		"search engine",
		"frc",
		"frc data steer",
		"vehicle",
		"vehicle 12704",
		"can topology --vehicle 12704",
		"db verify",
	} {
		args := append(strings.Fields(command), "--db", db)
		b.Run(command, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				if err := run(args); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
