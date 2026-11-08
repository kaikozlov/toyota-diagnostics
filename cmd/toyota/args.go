package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type options struct {
	db        string
	dbSet     bool
	region    string
	json      bool
	help      bool
	all       bool
	limit     int
	limitSet  bool
	literalAt int
}

// usageError is a mistake in how a command was invoked, as opposed to a
// failure while running it. It exits 2 and points at the command's usage.
type usageError struct {
	command string
	msg     string
}

func (e *usageError) Error() string { return e.msg }

func usagef(command, format string, args ...any) error {
	return &usageError{command: command, msg: fmt.Sprintf(format, args...)}
}

func parse(args []string) (options, []string, error) {
	opt := options{db: os.Getenv("TOYOTA_CATALOG"), region: "NA", limit: 50, literalAt: -1}
	if r := os.Getenv("TOYOTA_REGION"); r != "" {
		opt.region = r
	}
	var command []string
	for i := 0; i < len(args); i++ {
		token := args[i]
		if token == "--" {
			opt.literalAt = len(command)
			command = append(command, args[i+1:]...)
			break
		}
		var key, value string
		if n := strings.IndexByte(token, '='); n > 0 && strings.HasPrefix(token, "--") {
			key, value = token[:n], token[n+1:]
		} else {
			key = token
		}
		switch key {
		case "--db", "--region", "--limit":
			if !strings.Contains(token, "=") {
				i++
				if i >= len(args) {
					return opt, nil, usagef("", "%s requires a value", key)
				}
				value = args[i]
			}
			switch key {
			case "--db":
				opt.db = value
				opt.dbSet = true
			case "--region":
				opt.region = value
			case "--limit":
				n, err := strconv.Atoi(value)
				if err != nil || n <= 0 || n > 5000 {
					return opt, nil, usagef("", "--limit must be between 1 and 5000")
				}
				opt.limit, opt.limitSet = n, true
			}
		case "--json":
			if strings.Contains(token, "=") {
				return opt, nil, usagef("", "--json does not take a value")
			}
			opt.json = true
		case "--all":
			if strings.Contains(token, "=") {
				return opt, nil, usagef("", "--all does not take a value")
			}
			opt.all = true
		case "--help", "-h":
			opt.help = true
		default:
			command = append(command, token)
		}
	}
	return opt, command, nil
}

func findDB(explicit string) string {
	if explicit != "" {
		return explicit
	}
	if p := os.Getenv("TOYOTA_CATALOG"); p != "" {
		return p
	}
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	if root := os.Getenv("TOYOTA_DIAGNOSTICS_ROOT"); root != "" {
		candidates = append(candidates, root)
	}
	for _, start := range candidates {
		for dir := start; ; dir = filepath.Dir(dir) {
			p := filepath.Join(dir, "build", "catalog.sqlite")
			if info, err := os.Stat(p); err == nil && !info.IsDir() {
				return p
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
	}
	return ""
}

func encode(value any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	return enc.Encode(value)
}

var errNoCatalog = errors.New("runtime catalog not found; generate build/catalog.sqlite with `tools/gts bundle sqlite`")

// Native catalog browsing coexists with the original Python command contract.
// All live commands, mutation operations, and unsupported offline commands
// cross the existing bridge unchanged; no second Panda implementation exists.
func shouldBridge(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Profile and transport options select the existing Python runtime.
	for _, arg := range args {
		key, _, _ := strings.Cut(arg, "=")
		switch key {
		case "--registry", "--profile", "--vehicle", "--bus", "--transport", "--obd-multiplexing", "--j2534-library", "--j2534-device", "--j2534-baud":
			return true
		}
	}
	switch args[0] {
	case "monitor", "observe", "transport", "can", "did", "rid", "dtc", "uds", "ffd",
		"functional", "customize", "active-test", "utility", "health-check", "scan":
		return true
	case "vehicle":
		return len(args) > 1 && (args[1] == "detect" || args[1] == "mounted" || args[1] == "show")
	case "ecu":
		if len(args) > 2 {
			// Both ecu frc monitor and ecu monitor frc are live commands.
			return isLiveVerb(args[2]) || isLiveVerb(args[1]) || args[2] == "ffd"
		}
		return false
	case "search", "db", "browse", "inspect", "completion", "__complete":
		return false
	default:
		return len(args) > 1 && (isLiveVerb(args[1]) || args[1] == "ffd")
	}
}

func isLiveVerb(s string) bool { return s == "monitor" || s == "read" || s == "watch" }

// bareHelpRequest reports whether args is nothing but verb plus a help flag,
// so help for bridged commands can render from the Go table.
func bareHelpRequest(args []string, verb string) bool {
	sawVerb, sawHelp := false, false
	for _, a := range args {
		switch a {
		case verb:
			sawVerb = true
		case "--help", "-h":
			sawHelp = true
		default:
			return false
		}
	}
	return sawVerb && sawHelp
}

// Shared options have one placement contract even when the runtime owns the command.
func runtimeArgs(args []string) []string {
	var prefix, rest []string
	jsonOutput := false
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			rest = append(rest, args[i:]...)
			break
		}
		if arg == "--region" && i+1 < len(args) {
			prefix = append(prefix, arg, args[i+1])
			i++
		} else if strings.HasPrefix(arg, "--region=") {
			prefix = append(prefix, arg)
		} else if arg == "--json" {
			jsonOutput = true
		} else {
			rest = append(rest, arg)
		}
	}
	// The runtime's shorthand accepts noun-first ECU commands.
	if len(rest) >= 3 && rest[0] == "ecu" {
		switch rest[1] {
		case "read", "watch", "monitor":
			rest[1], rest[2] = rest[2], rest[1]
		}
	}
	if jsonOutput {
		rest = append(rest, "--json")
	}
	return append(prefix, rest...)
}
