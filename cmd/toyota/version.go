package main

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// version is the release label. `go build -ldflags "-X main.version=…"` can
// override it; otherwise an untagged checkout stays "devel".
var version = "devel"

func writeVersion(w io.Writer) error {
	_, err := fmt.Fprintln(w, versionString())
	return err
}

func versionString() string {
	label := version
	revision := ""
	dirty := false
	if info, ok := debug.ReadBuildInfo(); ok {
		if label == "devel" {
			if rel := releaseVersion(info.Main.Version); rel != "" {
				label = rel
			}
		}
		for _, s := range info.Settings {
			switch s.Key {
			case "vcs.revision":
				revision = s.Value
				if len(revision) > 7 {
					revision = revision[:7]
				}
			case "vcs.modified":
				dirty = s.Value == "true"
			}
		}
	}
	return formatVersion(label, revision, dirty)
}

func formatVersion(label, revision string, dirty bool) string {
	if dirty && revision != "" {
		revision += "-dirty"
	}
	if revision != "" {
		return "toyota " + label + " (" + revision + ")"
	}
	return "toyota " + label
}

// releaseVersion accepts a tagged module version and ignores Go's
// v0.0.0-timestamp-commit placeholders from untagged checkouts.
func releaseVersion(v string) string {
	if v == "" || v == "(devel)" || strings.HasPrefix(v, "v0.0.0-") {
		return ""
	}
	return v
}
