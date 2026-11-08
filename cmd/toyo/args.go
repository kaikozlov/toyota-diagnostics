package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/kaikozlov/toyota-diagnostics/internal/appenv"
)

type options struct {
	db        string
	dbSet     bool
	bundle    string
	region    string
	json      bool
	help      bool
	version   bool
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
	opt := options{db: appenv.Catalog(), bundle: appenv.Bundle(), region: "NA", limit: 50, literalAt: -1}
	if r := appenv.Region(); r != "" {
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
		case "--db", "--bundle", "--region", "--limit":
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
			case "--bundle":
				opt.bundle = value
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
		case "--version", "-V":
			if strings.Contains(token, "=") {
				return opt, nil, usagef("", "%s does not take a value", key)
			}
			opt.version = true
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
	if p := appenv.Catalog(); p != "" {
		return p
	}
	var candidates []string
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates, wd)
	}
	if exe, err := os.Executable(); err == nil {
		candidates = append(candidates, filepath.Dir(exe))
	}
	if root := appenv.Root(); root != "" {
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

var errNoCatalog = errors.New("runtime catalog not found (build/catalog.sqlite); generate it from ghidra_rh850_analysis with `tools/gts bundle sqlite --out build/catalog.sqlite`, or pass --db PATH")

func isLiveVerb(s string) bool { return s == "monitor" || s == "read" || s == "watch" }

// bareHelpRequest reports whether args is nothing but verb plus a help flag,
// so help for live commands can render from the Go table.
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
