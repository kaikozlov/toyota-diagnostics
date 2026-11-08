package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

var (
	cliExeOnce sync.Once
	cliExePath string
	cliExeErr  error
)

func compiledCLI(t *testing.T) string {
	t.Helper()
	cliExeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "toyota-cli-test")
		if err != nil {
			cliExeErr = err
			return
		}
		path := filepath.Join(dir, "toyota")
		cmd := exec.Command("go", "build", "-o", path, ".")
		if out, err := cmd.CombinedOutput(); err != nil {
			cliExeErr = err
			cliExePath = string(out)
			return
		}
		cliExePath = path
	})
	if cliExeErr != nil {
		t.Fatalf("go build: %v\n%s", cliExeErr, cliExePath)
	}
	return cliExePath
}

func runCLI(t *testing.T, env []string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	cmd := exec.Command(compiledCLI(t), args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	cmd.Env = append(os.Environ(), env...)
	err := cmd.Run()
	stdout, stderr = out.String(), errb.String()
	if err == nil {
		return stdout, stderr, 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return stdout, stderr, ee.ExitCode()
	}
	t.Fatalf("run %v: %v", args, err)
	return "", "", -1
}

func TestCLIProcessExitAndStreams(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	env := []string{"TOYOTA_CATALOG=/does/not/exist", "NO_COLOR=1"}

	stdout, stderr, code := runCLI(t, env, "--version")
	if code != 0 || !strings.HasPrefix(stdout, "toyota ") || stderr != "" {
		t.Fatalf("version: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, env, "--help")
	if code != 0 || !strings.Contains(stdout, "Explore") || stderr != "" {
		t.Fatalf("help: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, env, "search")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "search needs something to look for") || !strings.Contains(stderr, "usage:") {
		t.Fatalf("search usage: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, env, "--version", "search")
	if code != 2 || stdout != "" || !strings.Contains(stderr, "version takes no arguments") {
		t.Fatalf("version extra: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, env, "search", "LTA")
	if code != 1 || stdout != "" || !strings.Contains(stderr, "catalog not found") {
		t.Fatalf("missing catalog: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, env, "serach", "LTA")
	if code != 1 || stdout != "" || !strings.Contains(stderr, `unknown command "serach"`) || !strings.Contains(stderr, "did you mean") {
		t.Fatalf("typo: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, env, "didactic", "example")
	if code != 1 || stdout != "" || strings.Contains(stderr, "unknown command") || !strings.Contains(stderr, "catalog not found") {
		t.Fatalf("search phrase: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, append(env, "TOYOTA_REGION=bogus"), "completion", "zsh")
	if code != 0 || !strings.Contains(stdout, "#compdef toyota") || stderr != "" {
		t.Fatalf("completion region: code=%d out=%q err=%q", code, stdout, stderr)
	}

	stdout, stderr, code = runCLI(t, env, "__complete", "")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "search") || !strings.Contains(stdout, "monitor") {
		t.Fatalf("static complete: code=%d out=%q err=%q", code, stdout, stderr)
	}
	stdout, stderr, code = runCLI(t, env, "__complete", "-")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "--version") || !strings.Contains(stdout, "--json") {
		t.Fatalf("flag complete: code=%d out=%q err=%q", code, stdout, stderr)
	}
}

func TestCLIProcessFixtureOutput(t *testing.T) {
	cliFixture(t)
	catalogPath := os.Getenv("TOYOTA_CATALOG")
	base := []string{"TOYOTA_CATALOG=" + catalogPath, "TOYOTA_REGION="}

	stdout, stderr, code := runCLI(t, append(base, "NO_COLOR=1"), "search", "LTA")
	if code != 0 || stderr != "" {
		t.Fatalf("search: code=%d err=%q out=%q", code, stderr, stdout)
	}
	for _, want := range []string{"search", "LTA", "LTA Control Condition", "#1"} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("human search missing %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("NO_COLOR=1 should be plain:\n%q", stdout)
	}

	stdout, stderr, code = runCLI(t, append(base, "TERM=dumb", "NO_COLOR="), "search", "lta", "--json")
	if code != 0 || stderr != "" {
		t.Fatalf("json: code=%d err=%q out=%q", code, stderr, stdout)
	}
	var items []catalog.Item
	if err := json.Unmarshal([]byte(stdout), &items); err != nil || len(items) != 5 {
		t.Fatalf("search --json: %v %d %q", err, len(items), stdout)
	}

	stdout, stderr, code = runCLI(t, append(base, "NO_COLOR=0", "TERM=dumb"), "frc", "0x1202")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "LTA Installation Availability") {
		t.Fatalf("ident: code=%d err=%q out=%q", code, stderr, stdout)
	}
	if strings.Contains(stdout, "\x1b[") {
		t.Fatalf("TERM=dumb should be plain:\n%q", stdout)
	}

	stdout, stderr, code = runCLI(t, append(base, "NO_COLOR=1"), "didactic", "example")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Didactic example signal") {
		t.Fatalf("near-command search: code=%d err=%q out=%q", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI(t, append(base, "NO_COLOR=1"), "inspect", "3")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "LTA Installation Availability") {
		t.Fatalf("inspect: code=%d err=%q out=%q", code, stderr, stdout)
	}

	scoped, scopedErr, scopedCode := runCLI(t, append(base, "NO_COLOR=1"), "frc", "LTA", "control")
	stdout, stderr, code = runCLI(t, append(base, "NO_COLOR=1"), "inspect", "frc", "LTA", "control")
	if code != 0 || scopedCode != 0 || stdout != scoped || stderr != "" || scopedErr != "" {
		t.Fatalf("inspect scoped: code=%d/%d out=%q vs %q err=%q/%q", code, scopedCode, stdout, scoped, stderr, scopedErr)
	}

	stdout, stderr, code = runCLI(t, append(base, "NO_COLOR=1"), "version")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Software Version") {
		t.Fatalf("version search: code=%d err=%q out=%q", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI(t, append(base, "NO_COLOR=1"), "search", "1601")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "LTA Control Condition") || !strings.Contains(stdout, "0x1601") {
		t.Fatalf("hex search: code=%d err=%q out=%q", code, stderr, stdout)
	}

	stdout, stderr, code = runCLI(t, append(base, "NO_COLOR=1"), "search", "frc")
	if code != 0 || stderr != "" || !strings.Contains(stdout, "Nothing found") || !strings.Contains(stdout, "toyota frc opens") {
		t.Fatalf("ecu hint: code=%d err=%q out=%q", code, stderr, stdout)
	}
}
