package main

import (
	"strings"
	"testing"
)

func TestLiveUsageLineUsesLiveDocs(t *testing.T) {
	cases := map[string]string{
		"monitor": "toyo monitor ECU ITEM…",
		"observe": "toyo observe ECU:ITEM…",
		"did":     "toyo did read ECU DID…",
		"search":  "toyo search TERM…",
		"dtc":     "toyo dtc scan",
	}
	for command, want := range cases {
		if got := usageLine(command); got != want {
			t.Fatalf("%s: got %q want %q", command, got, want)
		}
	}
	err := usagef("monitor", "monitor needs an ECU and at least one DID/name")
	var u *usageError
	if !asUsage(err, &u) || usageLine(u.command) != cases["monitor"] {
		t.Fatalf("%v", err)
	}
}

func asUsage(err error, dest **usageError) bool {
	u, ok := err.(*usageError)
	if !ok {
		return false
	}
	*dest = u
	return true
}

func TestLiveHelpListsSupportedTransportFlags(t *testing.T) {
	t.Setenv("TOYOTA_CATALOG", "/does/not/exist")
	out, err := capture(func() error { return run([]string{"help", "monitor"}) })
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--j2534-device", "--j2534-baud", "--obd-multiplexing", "--j2534-library"} {
		if !strings.Contains(out, flag) {
			t.Fatalf("missing %s:\n%s", flag, out)
		}
	}
	for _, stale := range []string{"Python-owned", "bridges"} {
		if strings.Contains(out, stale) {
			t.Fatalf("stale %q in help:\n%s", stale, out)
		}
	}
}
