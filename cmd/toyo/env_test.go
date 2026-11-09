package main

import (
	"os"
	"strings"
	"testing"
)

// Clear a developer's TOYO_* settings so they don't leak into tests.
func TestMain(m *testing.M) {
	for _, kv := range os.Environ() {
		if key, _, _ := strings.Cut(kv, "="); strings.HasPrefix(key, "TOYO_") {
			os.Unsetenv(key)
		}
	}
	os.Exit(m.Run())
}
