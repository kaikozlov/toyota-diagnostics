package bridge

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBridgeTargetsInternalPythonRuntime(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "toyota_diag"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"pyproject.toml", "toyota_diag/__main__.py"} {
		if err := os.WriteFile(filepath.Join(root, path), []byte("# test\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("TOYOTA_DIAGNOSTICS_ROOT", root)
	got, err := sourceRoot()
	if err != nil || got != root {
		t.Fatal(got, err)
	}
	_, argv, env, err := command([]string{"dtc", "scan"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(argv[1:], " ") != "-m toyota_diag dtc scan" {
		t.Fatal(argv)
	}
	found := false
	for _, item := range env {
		if item == "TOYOTA_DIAGNOSTICS_ROOT="+root {
			found = true
		}
	}
	if !found {
		t.Fatal("bridge environment does not carry source root")
	}
}
