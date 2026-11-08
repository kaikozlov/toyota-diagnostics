// Command toyota is the native, indexed catalog CLI. Live operations reuse the
// existing Python diagnostic transport through an exec bridge.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"golang.org/x/term"

	"github.com/kaikozlov/toyota-diagnostics/internal/bridge"
	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
)

func run(args []string) error {
	if len(args) > 0 && args[0] == "__complete" {
		return runComplete(args[1:])
	}
	opt, rest, err := parse(args)
	if err != nil {
		return err
	}
	if len(rest) >= 2 && ((rest[0] == "did" && rest[1] == "list") || (rest[0] == "dtc" && rest[1] == "catalog")) {
		if len(rest) < 3 && !opt.help {
			return usagef("ecu", "%s %s needs an ECU, e.g. toyota %s %s frc", rest[0], rest[1], rest[0], rest[1])
		}
		action := "data"
		if rest[0] == "dtc" {
			action = "dtcs"
		}
		rest = append([]string{"ecu", action}, rest[2:]...)
	}
	if len(rest) > 0 && rest[0] == "help" {
		opt.help = true
		rest = rest[1:]
		args = append(append([]string{}, rest...), "--help")
	}
	// A bare help request for a bridged command answers from the Go command
	// table, in the house style. Anything deeper (operands, more flags) still
	// reaches the runtime's own --help.
	if opt.help && len(rest) == 1 {
		if _, live := liveDocs[rest[0]]; live && bareHelpRequest(args, rest[0]) {
			p := terminal.New(os.Stdout)
			commandHelp(p, rest[0])
			return nil
		}
	}
	if shouldBridge(rest) {
		if opt.dbSet {
			return usagef("", "--db selects the offline catalog; use --registry for runtime commands")
		}
		return bridge.Exec(runtimeArgs(args))
	}
	p := terminal.New(os.Stdout)
	if opt.help {
		return nativeHelp(p, rest)
	}
	if len(rest) == 0 && !opt.json && !term.IsTerminal(int(os.Stdin.Fd())) && !opt.version {
		overview(p)
		return nil
	}
	for i, arg := range rest {
		if opt.literalAt >= 0 && i >= opt.literalAt {
			break
		}
		if strings.HasPrefix(arg, "-") && arg != "-" {
			return usagef(firstWord(rest), "unknown option %q", arg)
		}
	}
	if err := versionRequest(opt, rest); err != nil {
		return err
	}
	if opt.version || (len(rest) == 1 && rest[0] == "version") {
		return writeVersion(os.Stdout)
	}
	region, err := catalog.Region(opt.region)
	if err != nil {
		return usagef("", "%v", err)
	}
	opt.region = region
	if err := usageWithoutCatalog(opt, rest); err != nil {
		return err
	}
	// Completion scripts are static; they should work on a fresh checkout.
	if len(rest) > 0 && rest[0] == "completion" {
		return (&app{p: p, opt: opt}).completion(rest[1:])
	}
	dbPath := findDB(opt.db)
	if dbPath == "" {
		if err := unknownCommandWithoutCatalog(rest); err != nil {
			return err
		}
		// Interactive first run: show the front door, then say how to get a catalog.
		if len(rest) == 0 && !opt.json {
			overview(p)
		}
		return errNoCatalog
	}
	info, err := os.Stat(dbPath)
	if err != nil {
		if os.IsNotExist(err) {
			if err := unknownCommandWithoutCatalog(rest); err != nil {
				return err
			}
			return fmt.Errorf("catalog not found at %s; generate it from ghidra_rh850_analysis or pass --db PATH", dbPath)
		}
		return fmt.Errorf("cannot read catalog %s: %w", dbPath, err)
	}
	if info.IsDir() {
		return fmt.Errorf("catalog path %s is a directory; pass the SQLite file with --db PATH", dbPath)
	}
	db, err := catalog.Open(dbPath)
	if err != nil {
		return err
	}
	defer db.Close()

	flush := func() {}
	if !opt.json && pageable(rest) && p.Color && term.IsTerminal(int(os.Stdout.Fd())) {
		var buf bytes.Buffer
		p.Out = &buf
		flush = func() { page(&buf, p.Height) }
	}
	err = runNative(context.Background(), db, p, opt, rest)
	flush()
	return err
}

func firstWord(args []string) string {
	if len(args) > 0 {
		return args[0]
	}
	return ""
}

func nativeHelp(p terminal.Printer, args []string) error {
	if len(args) == 0 {
		overview(p)
		return nil
	}
	if commandHelp(p, args[0]) {
		return nil
	}
	// Longer unknown words that look like a command typo get a correction.
	// Short collisions such as "frc" vs "ffd" are left alone.
	if hints := commandHints(args[0]); len(hints) > 0 && len(args[0]) >= 5 {
		return &catalog.NotFoundError{What: "command", Query: args[0], Suggestions: hints}
	}
	// Anything else uses the generic ECU shorthand page. That describes the
	// grammar (`toyota ECU …`) and does not assert the token exists in a
	// catalog, which is why `toyota help frc` stays useful offline.
	ecuShorthandHelp(p)
	return nil
}

// versionRequest reports extra operands given with --version/-V/version.
// A bare version request returns nil; the caller then prints the version.
func versionRequest(opt options, rest []string) error {
	if !opt.version && (len(rest) == 0 || rest[0] != "version") {
		return nil
	}
	extra := rest
	if len(extra) > 0 && extra[0] == "version" {
		extra = extra[1:]
	}
	if len(extra) > 0 {
		return usagef("version", "version takes no arguments")
	}
	return nil
}

// pageable reports whether a command prints a finite, read-once listing.
func pageable(args []string) bool {
	if len(args) == 0 {
		return false
	}
	switch args[0] {
	case "browse", "completion", "__complete":
		return false
	}
	return true
}

// page shows long output through $TOYOTA_PAGER, $PAGER or less, the way git
// does, and prints short output directly.
func page(buf *bytes.Buffer, height int) {
	direct := func() { _, _ = io.Copy(os.Stdout, buf) }
	if bytes.Count(buf.Bytes(), []byte("\n")) < height-1 {
		direct()
		return
	}
	command := os.Getenv("TOYOTA_PAGER")
	if command == "" {
		command = os.Getenv("PAGER")
	}
	if command == "" {
		command = "less"
	}
	fields := strings.Fields(command)
	if len(fields) == 0 || fields[0] == "cat" {
		direct()
		return
	}
	bin, err := exec.LookPath(fields[0])
	if err != nil {
		direct()
		return
	}
	cmd := exec.Command(bin, fields[1:]...)
	cmd.Stdin = bytes.NewReader(buf.Bytes())
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	if os.Getenv("LESS") == "" {
		// Quit if one screen, keep colours, leave the text on screen.
		cmd.Env = append(os.Environ(), "LESS=FRX")
	}
	// A pager the user quits early is not an error worth reporting.
	_ = cmd.Run()
}

// report prints err to stderr and returns the process exit code.
func report(err error) int {
	p := terminal.New(os.Stderr)
	p.Failure(err)
	var usage *usageError
	if errors.As(err, &usage) {
		if usage.command != "" {
			fmt.Fprintf(os.Stderr, "  usage: %s\n", usageLine(usage.command))
		}
		return 2
	}
	return 1
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		os.Exit(report(err))
	}
}
