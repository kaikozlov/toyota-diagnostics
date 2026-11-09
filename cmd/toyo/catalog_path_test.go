package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFindShippedCatalog(t *testing.T) {
	root := t.TempDir()
	t.Setenv("TOYO_CATALOG", "")
	t.Setenv("TOYOTA_CATALOG", "")
	t.Setenv("TOYO_ROOT", "")
	t.Setenv("TOYOTA_DIAGNOSTICS_ROOT", "")
	t.Chdir(root)
	if got := findDB(""); got != "" {
		t.Fatalf("unexpected catalog: %s", got)
	}
	if err := os.MkdirAll(filepath.Join(root, "catalog"), 0755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "catalog", "catalog.sqlite")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if got := findDB(""); got != path {
		t.Fatalf("%s != %s", got, path)
	}
	if got := findDB("override.sqlite"); got != "override.sqlite" {
		t.Fatal(got)
	}
	t.Setenv("TOYO_CATALOG", "environment.sqlite")
	if got := findDB(""); got != "environment.sqlite" {
		t.Fatal(got)
	}
}

func TestCheckoutRootWinsOverWorkingDirectory(t *testing.T) {
	t.Setenv("TOYO_CATALOG", "")
	t.Setenv("TOYOTA_CATALOG", "")
	root, other := t.TempDir(), t.TempDir()
	for _, dir := range []string{root, other} {
		if err := os.MkdirAll(filepath.Join(dir, "catalog"), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "catalog", "catalog.sqlite"), []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TOYO_ROOT", root)
	t.Chdir(other)
	if got, want := findDB(""), filepath.Join(root, "catalog", "catalog.sqlite"); got != want {
		t.Fatalf("%s != %s", got, want)
	}
}
