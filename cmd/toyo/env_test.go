package main

import (
	"os"
	"strings"
	"testing"
)

// Tests set the TOYOTA_* aliases; a developer's TOYO_* settings would win over
// them, so clear those first.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if key, _, _ := strings.Cut(kv, "="); strings.HasPrefix(key, "TOYO_") {
			os.Unsetenv(key)
		}
	}
	os.Exit(m.Run())
}
