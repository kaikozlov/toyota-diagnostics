package main

import (
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/appenv"
	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
)

func TestProductCommandIdentity(t *testing.T) {
	if terminal.Command != "toyo" || terminal.Product != "toyo-cli" {
		t.Fatalf("command=%q product=%q", terminal.Command, terminal.Product)
	}
	if got := formatVersion("devel", "abc1234", false); got != "toyo devel (abc1234)" {
		t.Fatalf("%q", got)
	}
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	out := output(t, "help")
	if !strings.Contains(out, "toyo search TERM") || strings.Contains(out, "toyota search") {
		t.Fatalf("help still uses the old command name:\n%s", out)
	}
	script := output(t, "completion", "zsh")
	if !strings.Contains(script, "#compdef toyo") || strings.Contains(script, "#compdef toyota") {
		t.Fatalf("zsh completion is not bound to toyo:\n%s", script)
	}
}

func TestToyoEnvOverridesLegacyAlias(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/legacy.sqlite")
	t.Setenv("TOYO_CATALOG", "/preferred.sqlite")
	if got := appenv.Catalog(); got != "/preferred.sqlite" {
		t.Fatalf("preferred catalog %q", got)
	}
	t.Setenv("TOYO_CATALOG", "")
	if got := appenv.Catalog(); got != "/legacy.sqlite" {
		t.Fatalf("legacy catalog %q", got)
	}
}
