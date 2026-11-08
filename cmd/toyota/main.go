// Command toyota is the native, indexed catalog CLI. Live operations reuse the
// existing Python diagnostic transport through an exec bridge.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/kaikozlov/toyota-diagnostics/internal/bridge"
	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/explorer"
	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
)

type options struct {
	db     string
	region string
	json   bool
	limit  int
}

func parse(args []string) (options, []string, error) {
	opt := options{db: os.Getenv("TOYOTA_CATALOG"), region: "NA", limit: 50}
	var command []string
	for i := 0; i < len(args); i++ {
		token := args[i]
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
					return opt, nil, fmt.Errorf("%s requires a value", key)
				}
				value = args[i]
			}
			switch key {
			case "--db":
				opt.db = value
			case "--region":
				opt.region = value
			case "--limit":
				n, err := strconv.Atoi(value)
				if err != nil || n <= 0 || n > 5000 {
					return opt, nil, fmt.Errorf("--limit must be between 1 and 5000")
				}
				opt.limit = n
			}
		case "--json":
			opt.json = true
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

func runNative(ctx context.Context, db *catalog.DB, opt options, args []string) error {
	p := terminal.New(os.Stdout)
	if len(args) == 0 {
		if opt.json {
			return fmt.Errorf("--json requires a command")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			p.Help()
			return nil
		}
		return explorer.Run(db, opt.region, os.Stdin, os.Stdout)
	}
	switch args[0] {
	case "search":
		if len(args) < 2 {
			return errors.New("search requires a term, e.g. toyota search LTA")
		}
		query := strings.Join(args[1:], " ")
		items, err := db.Search(ctx, opt.region, query, opt.limit)
		if err != nil {
			return err
		}
		if opt.json {
			return encode(items)
		}
		p.Search(query, opt.region, items)
		if len(items) == 0 {
			return errors.New("no matching Toyota catalog entries")
		}
		return nil
	case "browse":
		if opt.json {
			return errors.New("browse is interactive; use search --json for structured results")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			return errors.New("browse requires an interactive terminal")
		}
		return explorer.Run(db, opt.region, os.Stdin, os.Stdout)
	case "db":
		if len(args) < 2 || args[1] == "status" {
			s, err := db.Stats(ctx)
			if err != nil {
				return err
			}
			if opt.json {
				return encode(s)
			}
			p.Stats(db.Path, s)
			return nil
		}
		if args[1] == "verify" {
			if err := db.Verify(ctx); err != nil {
				return err
			}
			if opt.json {
				return encode(map[string]string{"status": "ok"})
			}
			fmt.Fprintln(os.Stdout, "Catalog integrity: ok")
			return nil
		}
		return fmt.Errorf("unknown database command %q", args[1])
	case "ecu":
		if len(args) == 1 || args[1] == "list" {
			query := ""
			if len(args) >= 3 {
				query = strings.Join(args[2:], " ")
			}
			items, err := db.ECUs(ctx, opt.region, query, opt.limit)
			if err != nil {
				return err
			}
			if opt.json {
				return encode(items)
			}
			p.ECUs(opt.region, items)
			return nil
		}
		if args[1] == "info" {
			if len(args) < 3 {
				return errors.New("ecu info requires an ECU name or category ID")
			}
			return ecuCommand(ctx, db, p, opt, args[2], "info", nil)
		}
		if len(args) >= 3 && isECUAction(args[1]) {
			return ecuCommand(ctx, db, p, opt, args[2], args[1], args[3:])
		}
		if len(args) >= 3 {
			return ecuCommand(ctx, db, p, opt, args[1], args[2], args[3:])
		}
		return ecuCommand(ctx, db, p, opt, args[1], "info", nil)
	case "vehicle":
		if len(args) == 1 || args[1] == "list" {
			query := ""
			if len(args) >= 3 {
				query = strings.Join(args[2:], " ")
			}
			vehicles, err := db.Vehicles(ctx, opt.region, query, opt.limit)
			if err != nil {
				return err
			}
			if opt.json {
				return encode(vehicles)
			}
			p.Vehicles(opt.region, vehicles)
			return nil
		}
		explicit := args[1] == "info"
		if explicit {
			if len(args) < 3 {
				return errors.New("vehicle info requires a Toyota type ID or name")
			}
			args = args[1:]
		}
		query := strings.Join(args[1:], " ")
		vehicles, err := db.Vehicles(ctx, opt.region, query, opt.limit)
		if err != nil {
			return err
		}
		if len(vehicles) == 0 {
			return fmt.Errorf("no Toyota vehicles match %q", query)
		}
		var exact []catalog.Vehicle
		for _, v := range vehicles {
			if strings.EqualFold(v.Name, query) || strconv.Itoa(v.Type) == query {
				exact = append(exact, v)
			}
		}
		if len(exact) == 1 {
			vehicles = exact
		}
		if len(vehicles) != 1 && !explicit {
			if opt.json {
				return encode(vehicles)
			}
			p.Vehicles(opt.region, vehicles)
			return nil
		}
		if len(vehicles) != 1 {
			return fmt.Errorf("ambiguous vehicle %q; use the Toyota type ID", query)
		}
		candidates, err := db.VehicleECUs(ctx, vehicles[0])
		if err != nil {
			return err
		}
		if opt.json {
			return encode(map[string]any{"vehicle": vehicles[0], "candidates": candidates})
		}
		p.Vehicle(vehicles[0], candidates)
		return nil
	default:
		// Direct ECU shorthand: toyota frc, toyota frc data LTA
		if len(args) == 1 {
			return ecuCommand(ctx, db, p, opt, args[0], "info", nil)
		}
		return ecuCommand(ctx, db, p, opt, args[0], args[1], args[2:])
	}
}

var ecuAction = map[string]string{
	"data": "did", "dtcs": "dtc", "active-tests": "active-test",
	"plugins": "plugin", "functions": "function", "utilities": "utility",
}

func isECUAction(s string) bool { _, ok := ecuAction[s]; return ok }

func ecuCommand(ctx context.Context, db *catalog.DB, p terminal.Printer, opt options, ref, action string, terms []string) error {
	ecu, err := db.ECU(ctx, opt.region, ref)
	if err != nil {
		return err
	}
	if action == "info" || action == "" {
		if opt.json {
			return encode(ecu)
		}
		p.ECU(ecu)
		return nil
	}
	kind, ok := ecuAction[action]
	if !ok {
		return fmt.Errorf("unknown offline ECU action %q", action)
	}
	if ecu.CatalogMember == "" {
		return fmt.Errorf("%s has no decoded catalog", ecu.Key)
	}
	items, err := db.ECUItems(ctx, ecu, kind, strings.Join(terms, " "), opt.limit)
	if err != nil {
		return err
	}
	if opt.json {
		// Consistent envelope for machine consumers.
		return encode(map[string]any{"ecu": ecu, "items": items, "total": len(items)})
	}
	p.Items(ecu, kind, items)
	return nil
}

// Native catalog browsing coexists with the original Python command contract.
// All live commands, mutation operations, and unsupported offline commands
// cross the existing bridge unchanged; no second Panda implementation exists.
func shouldBridge(args []string) bool {
	if len(args) == 0 {
		return false
	}
	// Explicit legacy runtime/global options belong to the Python parser.
	if strings.HasPrefix(args[0], "-") {
		return true
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
			return args[2] == "monitor" || args[2] == "read" || args[2] == "watch" ||
				args[1] == "monitor" || args[1] == "read" || args[1] == "watch"
		}
		return false
	case "search", "db", "browse":
		return false
	default:
		return len(args) > 1 && (args[1] == "monitor" || args[1] == "read" || args[1] == "watch" || args[1] == "ffd")
	}
}

func run(args []string) error {
	opt, rest, err := parse(args)
	if err != nil {
		return err
	}
	p := terminal.New(os.Stdout)
	if len(args) > 0 && (args[0] == "--help" || args[0] == "-h" || args[0] == "help") {
		p.Help()
		return nil
	}
	if shouldBridge(rest) {
		return bridge.Exec(args)
	}
	region, err := catalog.Region(opt.region)
	if err != nil {
		return err
	}
	opt.region = region
	dbPath := findDB(opt.db)
	if dbPath == "" {
		return errors.New("runtime catalog not found; generate build/catalog.sqlite with `tools/gts bundle sqlite`")
	}
	db, err := catalog.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()
	return runNative(context.Background(), db, opt, rest)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "toyota:", err)
		os.Exit(1)
	}
}
