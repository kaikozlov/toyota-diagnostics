package main

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
)

type runtimeOptions struct {
	values map[string][]string
	flags  map[string]bool
	json   bool
}

func (o runtimeOptions) value(key, fallback string) string {
	v := o.values[key]
	if len(v) > 0 {
		return v[len(v)-1]
	}
	return fallback
}
func (o runtimeOptions) flag(key string) bool { return o.flags[key] }
func (o runtimeOptions) duration(key string, fallback time.Duration) (time.Duration, error) {
	v := o.value(key, "")
	if v == "" {
		return fallback, nil
	}
	n, e := strconv.ParseFloat(v, 64)
	if e != nil || n < 0 {
		return 0, fmt.Errorf("--%s requires nonnegative seconds", key)
	}
	return time.Duration(n * float64(time.Second)), nil
}

var runtimeValues = strings.Fields("vehicle bus transport j2534-library j2534-device j2534-baud interval count duration window ecu subfunction sub-address rx-address rx-sub-address body-type kind hold poll-interval raw-value engineering-value choice value button option-record mask member-raw-value member-engineering-value member-choice query out compare vin save")
var runtimeFlags = strings.Fields("obd-multiplexing force execute changed jsonl csv no-clear no-identities no-identity verbose")

func hasWord(words []string, s string) bool {
	for _, w := range words {
		if w == s {
			return true
		}
	}
	return false
}
func parseRuntime(args []string) (runtimeOptions, []string, error) {
	o := runtimeOptions{values: map[string][]string{}, flags: map[string]bool{}}
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			rest = append(rest, args[i+1:]...)
			break
		}
		if !strings.HasPrefix(a, "--") {
			rest = append(rest, a)
			continue
		}
		k, v, eq := strings.Cut(strings.TrimPrefix(a, "--"), "=")
		if hasWord(runtimeFlags, k) {
			if eq {
				return o, nil, fmt.Errorf("--%s does not take a value", k)
			}
			o.flags[k] = true
			continue
		}
		if !hasWord(runtimeValues, k) {
			return o, nil, fmt.Errorf("unknown option --%s", k)
		}
		if !eq {
			i++
			if i >= len(args) {
				return o, nil, fmt.Errorf("--%s requires a value", k)
			}
			v = args[i]
		}
		o.values[k] = append(o.values[k], v)
	}
	return o, rest, nil
}
func runtimeWords(args []string) []string {
	var rest []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--") {
			k, _, eq := strings.Cut(strings.TrimPrefix(a, "--"), "=")
			if hasWord(runtimeValues, k) && !eq {
				i++
			}
			continue
		}
		rest = append(rest, a)
	}
	return rest
}
func runtimeCommand(args []string) bool {
	a := runtimeWords(args)
	if len(a) == 0 {
		return false
	}
	switch a[0] {
	case "monitor", "observe", "transport", "can", "did", "rid", "dtc", "uds", "ffd", "functional", "customize", "active-test", "utility", "health-check", "scan":
		return true
	case "vehicle":
		return len(a) > 1 && (a[1] == "show" || a[1] == "detect" || a[1] == "mounted")
	case "ecu":
		return len(a) > 2 && (isLiveVerb(a[1]) || isLiveVerb(a[2]) || a[2] == "ffd")
	default:
		return len(a) > 1 && (isLiveVerb(a[1]) || a[1] == "ffd")
	}
}
func normalizeRuntime(a []string) []string {
	if len(a) > 2 && a[0] == "ecu" {
		if isLiveVerb(a[1]) {
			a[1], a[2] = a[2], a[1]
		}
		a = a[1:]
	}
	if len(a) > 1 && isLiveVerb(a[1]) {
		if a[1] == "monitor" {
			return append([]string{"monitor", a[0]}, a[2:]...)
		}
		return append([]string{"did", a[1], a[0]}, a[2:]...)
	}
	if len(a) > 1 && a[0] == "frc" && a[1] == "ffd" {
		return a[1:]
	}
	return a
}

type runtimeApp struct {
	ctx       context.Context
	db        *catalog.DB
	profile   *diagnostic.Profile
	opt       runtimeOptions
	transport transport.CAN
	sessions  []*diagnostic.Session
}

func runRuntime(opt options, args []string) (resultErr error) {
	ro, rest, err := parseRuntime(args)
	if err != nil {
		return usagef(firstWord(runtimeWords(args)), "%v", err)
	}
	rest = normalizeRuntime(rest)
	ro.json = opt.json
	ro.flags["all"] = opt.all
	if ro.flags["no-identities"] {
		ro.flags["no-identity"] = true
	}
	if ro.value("out", "") != "" {
		ro.values["save"] = ro.values["out"]
	}
	ro.values["limit"] = []string{strconv.Itoa(opt.limit)}
	if opt.help {
		return nativeHelp(terminal.New(os.Stdout), rest)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()
	r := &runtimeApp{ctx: ctx, opt: ro}
	defer func() {
		cleanup, c := context.WithTimeout(context.Background(), 3*time.Second)
		defer c()
		for i := len(r.sessions) - 1; i >= 0; i-- {
			for _, message := range r.sessions[i].CleanupErrors {
				resultErr = errors.Join(resultErr, fmt.Errorf("session cleanup: %s", message))
			}
			if e := r.sessions[i].Close(cleanup); e != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("session cleanup: %w", e))
			}
		}
		if r.transport != nil {
			if e := r.transport.Close(); e != nil {
				resultErr = errors.Join(resultErr, fmt.Errorf("transport cleanup: %w", e))
			}
		}
	}()
	if len(rest) == 0 {
		return usagef("", "expected a diagnostic command")
	}
	if rest[0] == "transport" {
		return r.transportCommand(rest[1:])
	}
	if len(rest) > 1 && (rest[0] == "health-check" || rest[0] == "scan") && (rest[1] == "show" || rest[1] == "diff") {
		return r.snapshotCommands(rest)
	}
	if len(rest) == 3 && rest[0] == "dtc" && rest[1] == "decode" {
		n, e := runtimeInt(rest[2])
		if e != nil {
			return e
		}
		bits, e := diagnostic.DecodeStatusBits(n)
		if e != nil {
			return e
		}
		return r.output(map[string]any{"status": n, "bits": bits})
	}
	bus, err := runtimeInt(ro.value("bus", "0"))
	if err != nil || bus < 0 || bus > 3 {
		return fmt.Errorf("--bus must be 0..3")
	}
	// Explicit raw endpoints and passive captures do not need Toyota metadata.
	r.profile = &diagnostic.Profile{Bus: bus}
	if len(rest) >= 3 && rest[0] == "uds" && rest[1] == "raw" {
		if _, e := runtimeInt(rest[2]); e == nil {
			return r.rawCommand(rest)
		}
	}
	if len(rest) >= 2 && rest[0] == "can" && rest[1] == "sniff" {
		return r.canCommand(rest)
	}
	path := findDB(opt.db)
	if path == "" {
		return errNoCatalog
	}
	r.db, err = catalog.Open(path)
	if err != nil {
		return err
	}
	defer r.db.Close()
	if ro.value("vehicle", "") == "" && len(rest) >= 2 && rest[0] == "active-test" && (rest[1] == "list" || rest[1] == "groups") {
		region, e := catalog.Region(opt.region)
		if e != nil {
			return e
		}
		r.profile = &diagnostic.Profile{Region: region}
		return r.operationList(rest)
	}
	r.profile, err = diagnostic.NewProfile(ctx, r.db, opt.region, ro.value("vehicle", ""), bus)
	if err != nil {
		return err
	}
	switch rest[0] {
	case "active-test", "utility", "customize":
		return r.operations(rest)
	case "ffd", "health-check", "scan", "vehicle":
		return r.snapshotCommands(rest)
	case "did", "rid":
		return r.didCommand(rest)
	case "monitor", "observe":
		return r.monitorCommand(rest)
	case "dtc":
		return r.dtcCommand(rest)
	case "uds":
		return r.rawCommand(rest)
	case "can":
		return r.canCommand(rest)
	case "functional":
		return r.functionalCommand(rest)
	}
	return usagef(rest[0], "unknown diagnostic command")
}
func (r *runtimeApp) output(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetEscapeHTML(false)
	if !r.opt.json {
		enc.SetIndent("", "  ")
	}
	return enc.Encode(v)
}
func runtimeInt(s string) (int, error) {
	base := 10
	if strings.HasPrefix(strings.ToLower(s), "0x") {
		base = 0
	}
	n, e := strconv.ParseInt(s, base, 64)
	if e != nil {
		return 0, fmt.Errorf("invalid integer %q", s)
	}
	return int(n), nil
}
func runtimeHex(s string) ([]byte, error) {
	b, e := hex.DecodeString(strings.Join(strings.Fields(s), ""))
	if e != nil {
		return nil, fmt.Errorf("invalid hex payload %q", s)
	}
	return b, nil
}
func runtimeArray(v any) []any        { a, _ := v.([]any); return a }
func runtimeMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func runtimeRows(v any) []map[string]any {
	if a, ok := v.([]map[string]any); ok {
		return a
	}
	a, _ := v.([]any)
	out := make([]map[string]any, 0, len(a))
	for _, x := range a {
		if m := runtimeMap(x); m != nil {
			out = append(out, m)
		}
	}
	return out
}
func runtimeNumber(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	case int64:
		return int(n)
	case uint32:
		return int(n)
	case uint16:
		return int(n)
	case byte:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := runtimeInt(n)
		return i
	}
	return 0
}
func (r *runtimeApp) connect(passive bool) error {
	if r.opt.value("transport", "panda") == "j2534" && r.opt.flag("obd-multiplexing") {
		return fmt.Errorf("--obd-multiplexing is only supported by direct Panda")
	}
	if r.transport != nil {
		return nil
	}
	var err error
	switch backend := r.opt.value("transport", "panda"); backend {
	case "panda":
		r.transport, err = transport.OpenPanda(r.ctx, transport.PandaOptions{OBDMultiplexing: r.opt.flag("obd-multiplexing"), Passive: passive})
	case "j2534":
		baud, e := runtimeInt(r.opt.value("j2534-baud", "500000"))
		if e != nil {
			return e
		}
		r.transport, err = transport.OpenJ2534(r.ctx, transport.J2534Options{Library: r.opt.value("j2534-library", ""), Device: r.opt.value("j2534-device", ""), Baud: baud, Bus: r.profile.Bus})
	default:
		return fmt.Errorf("unknown transport %q; choose panda or j2534", backend)
	}
	return err
}
func (r *runtimeApp) ecu(query string) (diagnostic.EcuSpec, error) { return r.profile.LookupECU(query) }
func diagnosticRouteError(e diagnostic.EcuSpec) error {
	if !e.RouteResolved {
		return fmt.Errorf("%s has no resolved route; select --vehicle TYPE or detect the connected vehicle", e.Key)
	}
	switch e.TransportKind {
	case "", "iso15765-phase-family", "iso15765-29bit-normal-fixed", "legacy-unclassified":
	default:
		return fmt.Errorf("%s transport %s is not implemented", e.Key, e.TransportKind)
	}
	return nil
}
func (r *runtimeApp) client(e diagnostic.EcuSpec) (*transport.Client, error) {
	if err := diagnosticRouteError(e); err != nil {
		return nil, err
	}
	if err := r.connect(false); err != nil {
		return nil, err
	}
	rx := e.Address + 8
	if e.Address > 0x7ff {
		rx = (e.Address & 0x1fff0000) | ((e.Address & 0xff) << 8) | ((e.Address >> 8) & 0xff)
	}
	if response := runtimeNumber(e.Route["physical_response_address"]); response > 0 {
		rx = uint32(response)
	}
	return &transport.Client{CAN: r.transport, TxID: e.Address, RxID: rx, Bus: r.profile.Bus, TxExtension: e.SubAddr, RxExtension: e.SubAddr, Timeout: 350 * time.Millisecond, PendingTimeout: 2 * time.Second}, nil
}
func (r *runtimeApp) session(e diagnostic.EcuSpec, operation ...map[string]any) (*diagnostic.Session, error) {
	var row map[string]any
	if len(operation) > 0 {
		row = operation[0]
	}
	control, err := r.profile.SessionControl(r.ctx)
	if err != nil {
		return nil, err
	}
	timeout, pending, err := diagnostic.CommsetTimeouts(control, row)
	if err != nil {
		return nil, err
	}
	life, err := diagnostic.ParseLifecycle(control, e.CategoryID)
	if err != nil {
		return nil, err
	}
	c, err := r.client(e)
	if err != nil {
		return nil, err
	}
	c.Timeout, c.PendingTimeout = timeout, pending
	s := diagnostic.NewSession(c, life)
	r.sessions = append(r.sessions, s)
	return s, nil
}
func (r *runtimeApp) transportCommand(a []string) error {
	if r.opt.value("transport", "panda") == "j2534" && r.opt.flag("obd-multiplexing") {
		return fmt.Errorf("--obd-multiplexing is only supported by direct Panda")
	}
	if len(a) != 1 {
		return usagef("transport", "expected transport list or status")
	}
	if a[0] == "list" {
		return r.output(map[string]any{"backends": []string{"panda", "j2534"}, "j2534": transport.J2534Providers(r.opt.value("j2534-library", ""))})
	}
	if a[0] != "status" {
		return usagef("transport", "unknown action %q", a[0])
	}
	if r.opt.value("transport", "panda") == "j2534" {
		return r.output(transport.ProbeJ2534(transport.J2534Options{Library: r.opt.value("j2534-library", ""), Device: r.opt.value("j2534-device", "")}))
	}
	s, e := transport.PandaStatus(r.ctx, transport.PandaOptions{OBDMultiplexing: r.opt.flag("obd-multiplexing")})
	if e != nil {
		return e
	}
	return r.output(s)
}
func isTimeout(e error) bool { return errors.Is(e, context.DeadlineExceeded) }
