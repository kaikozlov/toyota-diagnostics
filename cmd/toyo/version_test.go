package main

import (
	"errors"
	"strings"
	"testing"
)

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }

func TestReleaseAndFormatVersion(t *testing.T) {
	if got := releaseVersion(""); got != "" {
		t.Fatal(got)
	}
	if got := releaseVersion("(devel)"); got != "" {
		t.Fatal(got)
	}
	if got := releaseVersion("v0.0.0-20261108045518-5cfe8ec96d0e"); got != "" {
		t.Fatal(got)
	}
	if got := releaseVersion("v1.4.6"); got != "v1.4.6" {
		t.Fatal(got)
	}
	if got := formatVersion("devel", "5cfe8ec", false); got != "toyo devel (5cfe8ec)" {
		t.Fatal(got)
	}
	if got := formatVersion("devel", "5cfe8ec", true); got != "toyo devel (5cfe8ec-dirty)" {
		t.Fatal(got)
	}
	if got := formatVersion("v1.2.3", "", false); got != "toyo v1.2.3" {
		t.Fatal(got)
	}
	// dirty is recorded even if the modified setting appears before revision
	if got := formatVersion("devel", "abcdef1", true); !strings.HasSuffix(got, "(abcdef1-dirty)") {
		t.Fatal(got)
	}
}

func TestWriteVersionReportsIOError(t *testing.T) {
	err := writeVersion(errWriter{err: errors.New("disk full")})
	if err == nil || !strings.Contains(err.Error(), "disk full") {
		t.Fatal(err)
	}
}

func TestVersionStringUsesOverrideLabel(t *testing.T) {
	orig := version
	version = "v9.9.9"
	defer func() { version = orig }()
	got := versionString()
	if !strings.HasPrefix(got, "toyo v9.9.9") || strings.Contains(got, "v0.0.0-") {
		t.Fatal(got)
	}
}
