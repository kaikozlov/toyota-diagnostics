package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"golang.org/x/term"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/explorer"
	"github.com/kaikozlov/toyota-diagnostics/internal/suggest"
	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
)

// app is one invocation of the offline commands.
type app struct {
	ctx context.Context
	db  *catalog.DB
	p   terminal.Printer
	opt options
}

// ecuActions maps every spelling people use to the catalog's record kind.
var ecuActions = map[string]string{
	"data": "did", "dids": "did", "signals": "did",
	"dtcs": "dtc", "dtc": "dtc", "codes": "dtc",
	"tests": "active-test", "test": "active-test", "active-tests": "active-test", "active-test": "active-test",
	"plugins": "plugin", "plugin": "plugin",
	"functions": "function", "function": "function",
	"utilities": "utility", "utility": "utility",
}

func isECUAction(s string) bool { _, ok := ecuActions[s]; return ok }

func runNative(ctx context.Context, db *catalog.DB, p terminal.Printer, opt options, args []string) error {
	a := &app{ctx: ctx, db: db, p: p, opt: opt}
	return a.dispatch(args)
}

func (a *app) dispatch(args []string) error {
	if len(args) == 0 {
		if a.opt.json {
			return usagef("", "--json needs a command, e.g. toyota search LTA --json")
		}
		if !term.IsTerminal(int(os.Stdin.Fd())) {
			overview(a.p)
			return nil
		}
		return explorer.Run(a.db, a.opt.region, os.Stdin, os.Stdout)
	}
	rest := args[1:]
	switch args[0] {
	case "kinds":
		if err := validateKinds(rest); err != nil {
			return err
		}
		return a.kinds(nil)
	case "search":
		return a.search(rest)
	case "inspect":
		return a.inspect(rest)
	case "browse":
		return a.browse(rest)
	case "db":
		return a.database(rest)
	case "ecu":
		return a.ecuCommand(rest)
	case "vehicle":
		return a.vehicle(rest)
	case "completion":
		return a.completion(rest)
	}
	return a.shorthand(args)
}

// limit fetches one extra row so "there is more" is known, not guessed.
func (a *app) probe() int { return a.opt.limit + 1 }

func trimItems(items []catalog.Item, limit int) ([]catalog.Item, bool) {
	if len(items) > limit {
		return items[:limit], true
	}
	return items, false
}

func (a *app) search(args []string) error {
	if err := validateSearch(args); err != nil {
		return err
	}
	query := strings.Join(args, " ")
	items, err := a.db.Search(a.ctx, a.opt.region, query, a.probe())
	if err != nil {
		return err
	}
	items, more := trimItems(items, a.opt.limit)
	if a.opt.json {
		return encode(items)
	}
	a.p.Search(query, a.opt.region, items, more)
	if len(items) == 0 {
		if ecu, ok, err := a.db.ExactECU(a.ctx, a.opt.region, query); err != nil {
			return err
		} else if ok {
			fmt.Fprintf(a.p.Out, "  toyota %s opens the %s.\n", ecu.Key, ecu.Name)
		}
	}
	return nil
}

func (a *app) inspect(args []string) error {
	if err := validateInspect(args); err != nil {
		return err
	}
	if len(args) >= 2 {
		return a.inECU(args[0], args[1:], false)
	}
	id, err := strconv.ParseInt(args[0], 10, 64)
	if err != nil || id <= 0 {
		return usagef("inspect", "%q is not a record number; use the #number from a search row", args[0])
	}
	item, err := a.db.Item(a.ctx, id)
	if err != nil {
		return err
	}
	if a.opt.json {
		return encode(item)
	}
	a.p.Item(item)
	return nil
}

func (a *app) browse(args []string) error {
	if err := validateBrowse(args, a.opt.json); err != nil {
		return err
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) || !term.IsTerminal(int(os.Stdout.Fd())) {
		return errors.New("browse needs an interactive terminal")
	}
	return explorer.Run(a.db, a.opt.region, os.Stdin, os.Stdout)
}

func (a *app) database(args []string) error {
	if err := validateDB(args); err != nil {
		return err
	}
	sub := "status"
	if len(args) == 1 {
		sub = args[0]
	}
	switch sub {
	case "status":
		s, err := a.db.Stats(a.ctx)
		if err != nil {
			return err
		}
		if a.opt.json {
			return encode(s)
		}
		a.p.Stats(a.db.Path, s)
		return nil
	case "verify":
		if err := a.db.Verify(a.ctx); err != nil {
			return err
		}
		if a.opt.json {
			return encode(map[string]string{"status": "ok"})
		}
		a.p.Verified()
		return nil
	}
	return usagef("db", "unknown db command %q", sub)
}

// ecuCommand handles `toyota ecu …`, in either word order.
func (a *app) ecuCommand(args []string) error {
	if len(args) == 0 || args[0] == "list" {
		return a.listECUs(strings.Join(args[min(1, len(args)):], " "))
	}
	if args[0] == "info" {
		if len(args) < 2 {
			return usagef("ecu", "ecu info needs an ECU name or category")
		}
		return a.inECU(strings.Join(args[1:], " "), nil, false)
	}
	// Verb first: toyota ecu data frc LTA.
	if len(args) >= 2 && isECUAction(args[0]) {
		return a.inECU(args[1], append([]string{args[0]}, args[2:]...), false)
	}
	return a.inECU(args[0], args[1:], true)
}

func (a *app) listECUs(query string) error {
	// Most of Toyota's declared ECUs have no decoded catalog and nothing to
	// browse; lead with the ones that do and say how many were left out.
	decodedOnly := !a.opt.all
	rows, err := a.db.ListECUs(a.ctx, a.opt.region, query, decodedOnly, a.probe())
	if err != nil {
		return err
	}
	if decodedOnly && len(rows) == 0 && query != "" {
		decodedOnly = false
		if rows, err = a.db.ListECUs(a.ctx, a.opt.region, query, false, a.probe()); err != nil {
			return err
		}
	}
	more := len(rows) > a.opt.limit
	if more {
		rows = rows[:a.opt.limit]
	}
	if a.opt.json {
		all, err := a.db.ECUs(a.ctx, a.opt.region, query, a.opt.limit)
		if err != nil {
			return err
		}
		return encode(all)
	}
	hidden := 0
	if decodedOnly {
		every, err := a.db.ListECUs(a.ctx, a.opt.region, query, false, 5000)
		if err != nil {
			return err
		}
		decoded := 0
		for _, e := range every {
			if e.CatalogMember != "" {
				decoded++
			}
		}
		hidden = len(every) - decoded
	}
	a.p.ECUs(a.opt.region, query, rows, hidden)
	return nil
}

func exactECU(e catalog.ECU, ref string) bool {
	return strings.EqualFold(e.Key, ref) || strings.EqualFold(e.Name, ref) || strconv.Itoa(e.CategoryID) == ref
}

// shorthand answers `toyota WORD …`. A known ECU opens that ECU; anything
// else is treated as a search, so a guess never dead-ends.
func (a *app) shorthand(args []string) error {
	ecu, err := a.db.ECU(a.ctx, a.opt.region, args[0])
	switch {
	case err == nil && (len(args) > 1 || exactECU(ecu, args[0])):
		return a.showECU(ecu, args[1:])
	case err != nil && !isLookupMiss(err):
		return err
	case len(args) > 1 && isECUAction(args[1]) && err != nil:
		// "toyota frcc data": the ECU was the thing being asked about.
		return err
	}
	return a.searchAnything(args)
}

func isLookupMiss(err error) bool {
	var nf *catalog.NotFoundError
	var amb *catalog.AmbiguousError
	return errors.As(err, &nf) || errors.As(err, &amb)
}

// searchAnything searches, and only when nothing matches explains what was
// understood and offers corrections.
func (a *app) searchAnything(args []string) error {
	query := strings.Join(args, " ")
	items, err := a.db.Search(a.ctx, a.opt.region, query, a.probe())
	if err != nil {
		return err
	}
	items, more := trimItems(items, a.opt.limit)
	if a.opt.json {
		return encode(items)
	}
	if len(items) > 0 {
		a.p.Search(query, a.opt.region, items, more)
		return nil
	}
	commands := commandHints(args[0])
	if len(commands) > 0 && len(args) > 1 {
		// "toyota serach LTA": the first word was meant as a command.
		return &catalog.NotFoundError{What: "command", Query: args[0], Suggestions: commands}
	}
	miss := &catalog.NotFoundError{Query: query, Suggestions: commands}
	if _, err := a.db.ECU(a.ctx, a.opt.region, args[0]); err != nil {
		var nf *catalog.NotFoundError
		if errors.As(err, &nf) {
			miss.Suggestions = append(miss.Suggestions, nf.Suggestions...)
		}
	}
	if len(miss.Suggestions) > 4 {
		miss.Suggestions = miss.Suggestions[:4]
	}
	return miss
}

// inECU resolves ref and runs rest inside it. With listOnMiss an unknown ECU
// shows what matched instead of failing.
func (a *app) inECU(ref string, rest []string, listOnMiss bool) error {
	ecu, err := a.db.ECU(a.ctx, a.opt.region, ref)
	if err != nil {
		if listOnMiss && len(rest) == 0 && isLookupMiss(err) {
			rows, lerr := a.db.ListECUs(a.ctx, a.opt.region, ref, false, a.probe())
			if lerr == nil && len(rows) > 0 {
				return a.listECUs(ref)
			}
		}
		return err
	}
	return a.showECU(ecu, rest)
}

// showECU is the grammar inside an ECU:
//
//	frc                  overview
//	frc data [QUERY]     one family of records
//	frc 0x1601           one record by identifier
//	frc free text        search within the ECU
func (a *app) showECU(ecu catalog.ECU, rest []string) error {
	if len(rest) == 0 || rest[0] == "info" {
		if len(rest) > 1 {
			return usagef("ecu", "ecu info takes no search term")
		}
		if a.opt.json {
			return encode(ecu)
		}
		a.p.ECU(ecu)
		return nil
	}
	if rest[0] == "kinds" {
		if len(rest) != 1 {
			return usagef("ecu", "kinds takes no arguments")
		}
		return a.kinds(&ecu)
	}
	if kind, ok := ecuActions[rest[0]]; ok {
		return a.family(ecu, kind, strings.Join(rest[1:], " "))
	}
	known, err := a.catalogKind(rest[0])
	if err != nil {
		return err
	}
	if known {
		return a.family(ecu, rest[0], strings.Join(rest[1:], " "))
	}
	if len(rest) == 1 {
		items, err := a.db.ItemsByIdentifier(a.ctx, ecu, rest[0])
		if err != nil {
			return err
		}
		if len(items) > 0 {
			return a.records(ecu, rest[0], items)
		}
	}
	return a.scoped(ecu, strings.Join(rest, " "))
}

func (a *app) family(ecu catalog.ECU, kind, query string) error {
	items, err := a.db.ECUItems(a.ctx, ecu, kind, query, a.probe())
	if err != nil {
		return err
	}
	items, more := trimItems(items, a.opt.limit)
	if a.opt.json {
		return encode(map[string]any{"ecu": ecu, "items": items, "shown": len(items)})
	}
	a.p.Items(ecu, kind, query, items, more)
	return nil
}

func (a *app) scoped(ecu catalog.ECU, query string) error {
	items, err := a.db.SearchECU(a.ctx, ecu, query, a.probe())
	if err != nil {
		return err
	}
	items, more := trimItems(items, a.opt.limit)
	if a.opt.json {
		return encode(map[string]any{"ecu": ecu, "items": items, "shown": len(items)})
	}
	a.p.Scoped(ecu, query, items, more)
	return nil
}

// records shows what an identifier means inside an ECU. One record opens in
// full; several signals sharing a DID open as a bit map.
func (a *app) records(ecu catalog.ECU, ident string, items []catalog.Item) error {
	details := make([]catalog.ItemDetail, 0, len(items))
	allDID := true
	for _, it := range items {
		d, err := a.db.Item(a.ctx, it.ID)
		if err != nil {
			return err
		}
		allDID = allDID && d.Kind == "did"
		details = append(details, d)
	}
	if a.opt.json {
		return encode(map[string]any{"ecu": ecu, "records": details})
	}
	switch {
	case len(details) == 1:
		a.p.Item(details[0])
	case allDID:
		a.p.DID(ecu, ident, details)
	default:
		a.p.Matches(ecu, ident, items)
	}
	return nil
}

func (a *app) vehicle(args []string) error {
	if len(args) > 0 && (args[0] == "list" || args[0] == "info") {
		args = args[1:]
	}
	query := strings.Join(args, " ")
	fetch := 20000
	vehicles, err := a.db.Vehicles(a.ctx, a.opt.region, query, fetch)
	if err != nil {
		return err
	}
	if len(vehicles) == 0 {
		return &catalog.NotFoundError{What: "vehicle", Query: query}
	}
	// A type number or an exact OEM name settles which vehicle was meant.
	chosen := vehicles
	var exact []catalog.Vehicle
	for _, v := range vehicles {
		if query != "" && (strconv.Itoa(v.Type) == query || strings.EqualFold(v.Name, query)) {
			exact = append(exact, v)
		}
	}
	if len(exact) > 0 {
		chosen = exact
	}
	groups := catalog.GroupVehicles(chosen)
	if query != "" && len(groups) == 1 {
		if len(chosen) == 1 {
			return a.vehicleDetail(chosen[0])
		}
		return a.variants(chosen)
	}
	if a.opt.json {
		return encode(chosen[:min(len(chosen), a.opt.limit)])
	}
	total := len(groups)
	if a.opt.limitSet && total > a.opt.limit {
		groups = groups[:a.opt.limit]
	}
	a.p.VehicleGroups(a.opt.region, query, groups, total)
	return nil
}

func (a *app) variants(vehicles []catalog.Vehicle) error {
	if a.opt.json {
		return encode(vehicles)
	}
	counts, err := a.db.VehicleECUCounts(a.ctx, a.opt.region)
	if err != nil {
		return err
	}
	a.p.VehicleVariants(a.opt.region, vehicles, counts)
	return nil
}

func (a *app) vehicleDetail(v catalog.Vehicle) error {
	candidates, err := a.db.VehicleECUs(a.ctx, v)
	if err != nil {
		return err
	}
	if a.opt.json {
		return encode(map[string]any{"vehicle": v, "candidates": candidates})
	}
	a.p.Vehicle(v, candidates)
	return nil
}

func validateSearch(args []string) error {
	if len(args) == 0 {
		return usagef("search", "search needs something to look for")
	}
	return nil
}

func validateInspect(args []string) error {
	if len(args) == 0 {
		return usagef("inspect", "inspect needs a record number")
	}
	if len(args) == 1 {
		id, err := strconv.ParseInt(args[0], 10, 64)
		if err != nil || id <= 0 {
			return usagef("inspect", "%q is not a record number; use the #number from a search row", args[0])
		}
	}
	return nil
}

func validateBrowse(args []string, jsonOutput bool) error {
	if len(args) != 0 {
		return usagef("browse", "browse takes no arguments")
	}
	if jsonOutput {
		return usagef("browse", "browse is interactive; use search --json for structured results")
	}
	return nil
}

func validateCompletion(args []string) error {
	if len(args) != 1 {
		return usagef("completion", "completion needs a shell: zsh, bash or fish")
	}
	switch args[0] {
	case "zsh", "bash", "fish":
		return nil
	default:
		return usagef("completion", "unsupported shell %q; choose zsh, bash or fish", args[0])
	}
}

func validateKinds(args []string) error {
	if len(args) != 0 {
		return usagef("kinds", "kinds takes no arguments")
	}
	return nil
}

func validateDB(args []string) error {
	if len(args) > 1 {
		return usagef("db", "db takes status or verify")
	}
	if len(args) == 1 && args[0] != "status" && args[0] != "verify" {
		return usagef("db", "unknown db command %q", args[0])
	}
	return nil
}

// validateOffline reports invocation mistakes that do not need the catalog,
// using the same helpers as the command implementations.
func validateOffline(opt options, rest []string) error {
	if len(rest) == 0 {
		if opt.json {
			return usagef("", "--json needs a command, e.g. toyota search LTA --json")
		}
		return nil
	}
	switch rest[0] {
	case "search":
		return validateSearch(rest[1:])
	case "inspect":
		return validateInspect(rest[1:])
	case "browse":
		return validateBrowse(rest[1:], opt.json)
	case "kinds":
		return validateKinds(rest[1:])
	case "db":
		return validateDB(rest[1:])
	}
	return nil
}

// unknownCommandWithoutCatalog offers a command correction when there is no
// catalog to search. Single-word invocations are left alone so they stay
// search/ECU fallbacks once a catalog is available.
func unknownCommandWithoutCatalog(rest []string) error {
	if len(rest) < 2 {
		return nil
	}
	if _, ok := nativeDocs[rest[0]]; ok {
		return nil
	}
	if _, ok := liveDocs[rest[0]]; ok {
		return nil
	}
	if hints := commandHints(rest[0]); len(hints) > 0 {
		return &catalog.NotFoundError{What: "command", Query: rest[0], Suggestions: hints}
	}
	return nil
}

func commandHints(query string) []string {
	if len([]rune(query)) < 5 {
		return nil
	}
	return suggest.Typos(query, commandNames(), 2)
}
