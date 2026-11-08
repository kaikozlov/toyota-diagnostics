package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
)

const zshCompletion = `#compdef toyota
_toyota() {
  local out
  out=$(toyota __complete "${(@)words[2,CURRENT-1]}" "${words[CURRENT]}" 2>/dev/null) || return
  [[ -z $out ]] && return
  local -a items=("${(@)${(@f)out}/$'\t'/:}")
  _describe -t toyota 'toyota' items
}
if [[ ${funcstack[1]} == _toyota ]]; then _toyota "$@"; else compdef _toyota toyota; fi
`

const bashCompletion = `_toyota() {
  local cur=${COMP_WORDS[COMP_CWORD]}
  local IFS=$'\n'
  COMPREPLY=($(toyota __complete "${COMP_WORDS[@]:1:COMP_CWORD-1}" "$cur" 2>/dev/null | cut -f1))
}
complete -F _toyota toyota
`

const fishCompletion = `function __toyota_complete
    set -l tokens (commandline -opc)[2..-1]
    toyota __complete $tokens (commandline -ct) 2>/dev/null
end
complete -c toyota -f -a '(__toyota_complete)'
`

// runComplete answers a shell's completion request. The words are the
// user's half-typed command line, so they bypass option parsing, and any
// failure is silent: a prompt must never fill with errors.
func runComplete(words []string) error {
	region := "NA"
	for i, w := range words {
		if w == "--region" && i+1 < len(words) {
			region = words[i+1]
		} else if strings.HasPrefix(w, "--region=") {
			region = strings.TrimPrefix(w, "--region=")
		}
	}
	if r, err := catalog.Region(region); err == nil {
		region = r
	} else {
		region = "NA"
	}
	p := terminal.Printer{Out: os.Stdout}
	a := &app{ctx: context.Background(), p: p, opt: options{region: region, limit: 50}}
	if dbPath := findDB(os.Getenv("TOYOTA_CATALOG")); dbPath != "" {
		if db, err := catalog.Open(dbPath); err == nil {
			defer db.Close()
			a.db = db
			return a.complete(words)
		}
	}
	return a.complete(words)
}

func (a *app) completion(args []string) error {
	if err := validateCompletion(args); err != nil {
		return err
	}
	scripts := map[string]string{"zsh": zshCompletion, "bash": bashCompletion, "fish": fishCompletion}
	fmt.Fprint(a.p.Out, scripts[args[0]])
	return nil
}

type candidate struct{ name, note string }

// complete prints "name<TAB>description" lines for the word being typed.
// Failures print nothing: a completion must never write noise to a prompt.
func (a *app) complete(args []string) error {
	if len(args) == 0 {
		args = []string{""}
	}
	words, cur := args[:len(args)-1], args[len(args)-1]
	// Split the words typed so far into positionals and a pending option value.
	var positional []string
	pending := ""
	for i := 0; i < len(words); i++ {
		w := words[i]
		switch {
		case w == "--region" || w == "--limit" || w == "--db":
			if i == len(words)-1 {
				pending = w
			} else {
				i++
			}
		case strings.HasPrefix(w, "-"):
		default:
			positional = append(positional, w)
		}
	}
	prev := pending
	var out []candidate
	switch {
	case prev == "--region":
		out = []candidate{{"NA", "North America"}, {"EU", "Europe"}, {"JP", "Japan"}}
	case prev == "--limit" || prev == "--db":
	case strings.HasPrefix(cur, "-"):
		out = []candidate{{"--region", "NA, EU or JP"}, {"--limit", "rows to show"}, {"--json", "structured output"}, {"--all", "include undecoded ECUs"}, {"--db", "alternate catalog"}, {"--version", "print the CLI version"}, {"--help", "show help"}}
	default:
		out = a.candidates(positional)
	}
	seen := map[string]bool{}
	for _, c := range out {
		if !strings.HasPrefix(strings.ToLower(c.name), strings.ToLower(cur)) || seen[c.name] {
			continue
		}
		seen[c.name] = true
		if c.note != "" {
			fmt.Fprintf(a.p.Out, "%s\t%s\n", c.name, strings.ReplaceAll(c.note, "\n", " "))
		} else {
			fmt.Fprintln(a.p.Out, c.name)
		}
	}
	return nil
}

func (a *app) ecuCandidates() []candidate {
	if a.db == nil {
		return nil
	}
	rows, err := a.db.ListECUs(a.ctx, a.opt.region, "", true, 5000)
	if err != nil {
		return nil
	}
	out := make([]candidate, 0, len(rows))
	for _, e := range rows {
		out = append(out, candidate{e.Key, e.Name})
	}
	return out
}

var ecuActionNotes = []candidate{
	{"data", "Data List signals"}, {"dtcs", "trouble codes"}, {"tests", "Active Tests"},
	{"functions", "functions"}, {"plugins", "plugin bindings"}, {"utilities", "utilities"},
	{"monitor", "live Data List"}, {"read", "live DID read"}, {"watch", "live DID watch"},
}

func (a *app) candidates(words []string) []candidate {
	if len(words) == 0 {
		var out []candidate
		for _, n := range commandNames() {
			note := nativeDocs[n].summary
			out = append(out, candidate{n, note})
		}
		return append(out, a.ecuCandidates()...)
	}
	switch words[0] {
	case "help":
		if len(words) == 1 {
			var out []candidate
			for _, n := range commandNames() {
				out = append(out, candidate{n, nativeDocs[n].summary})
			}
			return out
		}
	case "completion":
		if len(words) == 1 {
			return []candidate{{"zsh", ""}, {"bash", ""}, {"fish", ""}}
		}
	case "db":
		if len(words) == 1 {
			return []candidate{{"status", "catalog provenance and size"}, {"verify", "SQLite integrity check"}}
		}
	case "vehicle":
		if len(words) == 1 {
			if a.db == nil {
				return []candidate{{"detect", "live: identify the connected car"}, {"mounted", "live: ECUs that answer"}}
			}
			vehicles, err := a.db.Vehicles(a.ctx, a.opt.region, "", 20000)
			if err != nil {
				return nil
			}
			var out []candidate
			for _, g := range groupNames(vehicles) {
				out = append(out, candidate{g, ""})
			}
			return append(out, candidate{"detect", "live: identify the connected car"}, candidate{"mounted", "live: ECUs that answer"})
		}
	case "ecu":
		switch len(words) {
		case 1:
			return append([]candidate{{"list", "find ECUs"}, {"info", "one ECU at a glance"}}, a.ecuCandidates()...)
		case 2:
			if words[1] == "info" || words[1] == "list" {
				if words[1] == "info" {
					return a.ecuCandidates()
				}
				return nil
			}
			return ecuActionNotes
		}
	case "search", "inspect", "browse":
	default:
		if len(words) == 1 {
			return ecuActionNotes
		}
	}
	return nil
}

func groupNames(vehicles []catalog.Vehicle) []string {
	seen := map[string]bool{}
	var out []string
	for _, v := range vehicles {
		if !seen[v.Name] {
			seen[v.Name] = true
			out = append(out, v.Name)
		}
	}
	return out
}
