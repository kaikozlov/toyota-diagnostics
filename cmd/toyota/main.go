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
		nativeHelp(p, rest)
		return nil
	}
	if len(rest) == 0 && !opt.json && !term.IsTerminal(int(os.Stdin.Fd())) {
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
	region, err := catalog.Region(opt.region)
	if err != nil {
		return usagef("", "%v", err)
	}
	opt.region = region
	dbPath := findDB(opt.db)
	if dbPath == "" {
		return errNoCatalog
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

func nativeHelp(p terminal.Printer, args []string) {
	if len(args) == 0 {
		overview(p)
		return
	}
	if commandHelp(p, args[0]) {
		return
	}
	ecuShorthandHelp(p)
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
