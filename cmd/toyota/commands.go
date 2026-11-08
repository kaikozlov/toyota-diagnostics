package main

import (
	"sort"
	"strings"

	"github.com/kaikozlov/toyota-diagnostics/internal/terminal"
)

type commandDoc struct {
	summary  string
	usage    []string
	examples []terminal.HelpEntry
	options  []terminal.HelpEntry
}

var regionOpt = terminal.HelpEntry{Usage: "--region NA|EU|JP", Summary: "GTS+ region (default NA, or $TOYOTA_REGION)"}
var jsonOpt = terminal.HelpEntry{Usage: "--json", Summary: "structured output for scripts"}
var limitOpt = terminal.HelpEntry{Usage: "--limit N", Summary: "rows to show (default 50)"}

// nativeDocs describes the offline commands. Names here are also the
// completion and did-you-mean vocabulary.
var nativeDocs = map[string]commandDoc{
	"search": {
		summary: "search every signal, code, test, ECU and vehicle",
		usage:   []string{"toyota search TERM…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota search LTA", Summary: "everything Toyota calls LTA"},
			{Usage: "toyota search 1601", Summary: "the same DID as 0x1601"},
			{Usage: "toyota search steering angle", Summary: "all words must match"},
			{Usage: "toyota frc LTA", Summary: "the same, inside one ECU"},
		},
		options: []terminal.HelpEntry{regionOpt, limitOpt, jsonOpt},
	},
	"inspect": {
		summary: "show one record by its search-result number",
		usage:   []string{"toyota inspect ID", "toyota inspect ECU IDENT|TEXT…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota inspect 161299", Summary: "the number at the end of a search row"},
			{Usage: "toyota inspect frc 0x1601", Summary: "same as toyota frc 0x1601"},
			{Usage: "toyota inspect frc LTA control", Summary: "same as toyota frc LTA control"},
		},
		options: []terminal.HelpEntry{jsonOpt},
	},
	"ecu": {
		summary: "list ECUs, or work inside one",
		usage: []string{
			"toyota ecu [QUERY]",
			"toyota ECU",
			"toyota ECU data|dtcs|tests|functions|plugins|utilities [QUERY]",
			"toyota ECU IDENT",
			"toyota ECU TEXT…",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyota ecu brake", Summary: "ECUs whose key or name has \"brake\""},
			{Usage: "toyota frc", Summary: "one ECU at a glance (key, name or category)"},
			{Usage: "toyota frc data steer", Summary: "its Data List, filtered"},
			{Usage: "toyota frc dtcs U0131", Summary: "its trouble codes"},
			{Usage: "toyota frc 0x1601", Summary: "decode one DID, DTC or Active Test"},
			{Usage: "toyota frc LTA control", Summary: "search just this ECU"},
		},
		options: []terminal.HelpEntry{
			{Usage: "--all", Summary: "include ECUs that have no decoded catalog"},
			regionOpt, limitOpt, jsonOpt,
		},
	},
	"vehicle": {
		summary: "browse Toyota vehicles and their declared ECUs",
		usage:   []string{"toyota vehicle [NAME|TYPE]", "toyota vehicle detect|mounted"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota vehicle", Summary: "every model, grouped"},
			{Usage: "toyota vehicle Camry", Summary: "its variants"},
			{Usage: "toyota vehicle 10041", Summary: "one type and its install-set ECUs"},
			{Usage: "toyota vehicle detect", Summary: "live: identify the connected car"},
		},
		options: []terminal.HelpEntry{regionOpt, limitOpt, jsonOpt},
	},
	"browse": {
		summary: "open the interactive explorer",
		usage:   []string{"toyota", "toyota browse"},
		options: []terminal.HelpEntry{regionOpt},
	},
	"db": {
		summary: "inspect the offline catalog",
		usage:   []string{"toyota db [status]", "toyota db verify"},
		options: []terminal.HelpEntry{
			{Usage: "--db PATH", Summary: "use another catalog (or $TOYOTA_CATALOG)"},
			jsonOpt,
		},
	},
	"completion": {
		summary: "shell completion that knows your ECUs",
		usage:   []string{"toyota completion zsh|bash|fish"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota completion zsh > ~/.zfunc/_toyota", Summary: "zsh (add ~/.zfunc to fpath)"},
			{Usage: "source <(toyota completion bash)", Summary: "bash"},
			{Usage: "toyota completion fish > ~/.config/fish/completions/toyota.fish", Summary: "fish"},
		},
	},
	"kinds": {
		summary: "list the record kinds present in the catalog",
		usage:   []string{"toyota kinds", "toyota ECU kinds"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota kinds", Summary: "every kind in this region"},
			{Usage: "toyota frc kinds", Summary: "kinds this ECU actually has"},
		},
		options: []terminal.HelpEntry{regionOpt, jsonOpt},
	},
}

var liveLinkOpts = []terminal.HelpEntry{
	{Usage: "--vehicle TYPE|NAME", Summary: "vehicle scope; live commands resolve it from the VIN when omitted"},
	{Usage: "--transport panda|j2534", Summary: "vehicle link backend (default panda)"},
	{Usage: "--j2534-library PATH", Summary: "J2534 provider library, discovered when omitted"},
	regionOpt,
}

var monitorOutputOpts = terminal.HelpEntry{Usage: "--jsonl | --csv", Summary: "one JSON object per sample group, or one CSV row per signal"}

// liveDocs gives the Python-owned commands a house-style help page. Deep
// flags still answer from the runtime: `toyota CMD ARG --help` bridges.
var liveDocs = map[string]commandDoc{
	"monitor": {
		summary: "watch decoded Data List values, live",
		usage:   []string{"toyota monitor ECU ITEM…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota monitor engine BATT", Summary: "every Data List signal Toyota calls BATT"},
			{Usage: "toyota monitor frc 0x1014 steer", Summary: "mix DID numbers and names"},
			{Usage: "toyota monitor frc data --interval 1", Summary: "whole Data List, once a second"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--interval S", Summary: "seconds between samples (default 0.25)"},
			{Usage: "--count N", Summary: "stop after N samples; 0 runs until interrupted"},
			{Usage: "--changed", Summary: "print only signals whose value changed"},
			monitorOutputOpts,
			{Usage: "--no-clear", Summary: "append samples instead of redrawing the terminal"},
		}, liveLinkOpts...),
	},
	"observe": {
		summary: "monitor Data List values across several ECUs at once",
		usage:   []string{"toyota observe ECU:ITEM…", "toyota observe PRESET"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota observe engine:BATT frc:steer", Summary: "any ECUs, DID numbers or terms"},
			{Usage: "toyota observe tss3-longitudinal", Summary: "a recovered preset"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--interval S", Summary: "seconds between samples (default 0.25)"},
			{Usage: "--count N", Summary: "stop after N samples; 0 runs until interrupted"},
			{Usage: "--changed", Summary: "print only signals whose value changed"},
			monitorOutputOpts,
			{Usage: "--no-clear", Summary: "append samples instead of redrawing the terminal"},
		}, liveLinkOpts...),
	},
	"health-check": {
		summary: "all-system Health Check over the vehicle's install set (scan is an alias)",
		usage:   []string{"toyota scan", "toyota health-check"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota scan", Summary: "mount states, DTCs, freeze frames, RoBs, identities"},
			{Usage: "toyota health-check --out run.json", Summary: "also save the full snapshot"},
			{Usage: "toyota health-check --compare run.json", Summary: "diff against a saved run"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--out FILE", Summary: "write the complete JSON snapshot"},
			{Usage: "--compare FILE", Summary: "diff against a saved Health Check"},
			{Usage: "--no-identities", Summary: "skip exact generic-CID identity reads"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"transport": {
		summary: "show the vehicle-link status, or list transport backends",
		usage:   []string{"toyota transport status", "toyota transport list"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota transport status", Summary: "backend, readiness and detail"},
			{Usage: "toyota transport list", Summary: "backends and discoverable J2534 providers"},
		},
		options: []terminal.HelpEntry{
			{Usage: "--json", Summary: "structured output for scripts"},
			{Usage: "--transport panda|j2534", Summary: "backend to report on (default panda)"},
			{Usage: "--j2534-library PATH", Summary: "J2534 provider library, discovered when omitted"},
		},
	},
	"can": {
		summary: "GTS CAN topology, or capture raw frames",
		usage:   []string{"toyota can topology", "toyota can sniff ADDRESS…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota can topology", Summary: "the registry's bus map"},
			{Usage: "toyota can sniff 0x7E0", Summary: "capture one address"},
		},
		options: []terminal.HelpEntry{
			{Usage: "--duration S", Summary: "capture window (default 5; 0 runs until interrupted)"},
			{Usage: "--count N", Summary: "stop after N matching frames"},
			{Usage: "--bus N", Summary: "Panda bus (default: the profile's diagnostic bus)"},
			jsonOpt,
		},
	},
	"did": {
		summary: "live DID reads, support contracts and hex decoding",
		usage: []string{
			"toyota did read ECU DID…",
			"toyota did watch ECU DID…",
			"toyota did support ECU [DID…]",
			"toyota did decode ECU DID PAYLOAD",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyota did read engine 0x1002", Summary: "one live read, decoded"},
			{Usage: "toyota did watch frc 0x1014 --interval 1", Summary: "repeat and watch"},
			{Usage: "toyota did decode frc 0x1202 01", Summary: "decode hex bytes offline"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--interval S", Summary: "watch seconds between samples (default 0.25)"},
			{Usage: "--count N", Summary: "watch samples; 0 runs until interrupted"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"rid": {
		summary: "live routine-identifier capability queries",
		usage:   []string{"toyota rid support ECU [RID…]"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota rid support engine", Summary: "every advertised RID"},
		},
		options: append([]terminal.HelpEntry{jsonOpt}, liveLinkOpts...),
	},
	"dtc": {
		summary: "scan or clear trouble codes; decode a status byte",
		usage:   []string{"toyota dtc scan", "toyota dtc clear", "toyota dtc decode STATUS"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota dtc scan", Summary: "every responding ECU, decoded descriptions"},
			{Usage: "toyota dtc scan --ecu engine", Summary: "one ECU (repeatable)"},
			{Usage: "toyota dtc decode 0x2F", Summary: "explain a status byte offline"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--all", Summary: "scan: include non-fault-status records"},
			{Usage: "--ecu KEY", Summary: "scan: limit to one ECU (repeatable)"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"uds": {
		summary: "send one raw UDS request",
		usage:   []string{"toyota uds raw ECU SERVICE [DATA]"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota uds raw frc 0x22 0x1202", Summary: "one request, response printed as hex"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--force", Summary: "acknowledge a mutating request"},
			{Usage: "--sub-address BYTE", Summary: "ISO-TP address-extension bytes"},
		}, liveLinkOpts...),
	},
	"ffd": {
		summary: "TSS3 freeze frames: browse definitions, read Operation/Image FFD",
		usage: []string{
			"toyota ffd data|robs [QUERY]",
			"toyota ffd operation list|records|read …",
			"toyota ffd image info|list|read …",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyota ffd data steer", Summary: "Operation-FFD signal definitions"},
			{Usage: "toyota ffd operation read 0x9D 1", Summary: "one stored Operation record, decoded"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--query TERM", Summary: "read: only blocks matching a DID/name"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"functional": {
		summary: "broadcast OBD-mode requests to all ECUs",
		usage:   []string{"toyota functional obd MODE [PAYLOAD]"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota functional obd 09 02", Summary: "ask every ECU for its VIN"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--window S", Summary: "seconds to listen (default 1)"},
			{Usage: "--force", Summary: "acknowledge a mutating request"},
		}, liveLinkOpts...),
	},
	"customize": {
		summary: "browse and change OEM Customize settings",
		usage: []string{
			"toyota customize groups [QUERY]",
			"toyota customize items|info GROUP [ITEM]",
			"toyota customize read GROUP ITEM",
			"toyota customize set GROUP ITEM VALUE",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyota customize groups lamp", Summary: "find a group"},
			{Usage: "toyota customize read body lamp_turn_timing", Summary: "one live setting"},
			{Usage: "toyota customize set body lamp_turn_timing short", Summary: "dry-run plan; --execute writes"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--body-type N", Summary: "installation body variant"},
			{Usage: "--execute", Summary: "set: acknowledge the write; omitted is a dry run"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"active-test": {
		summary: "browse, plan, run or stop recovered Active Tests",
		usage: []string{
			"toyota active-test list [ECU]",
			"toyota active-test plan ECU ITEM",
			"toyota active-test run|stop ECU ITEM",
			"toyota active-test groups|group-run|group-stop …",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyota active-test list frc", Summary: "one ECU's tests"},
			{Usage: "toyota active-test plan frc 0xA429", Summary: "the zero-transmit plan"},
			{Usage: "toyota active-test run frc 0xA429", Summary: "dry run; --execute actuates"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--hold S", Summary: "run: seconds before stop (default 1)"},
			{Usage: "--raw-value N | --engineering-value V | --choice NAME", Summary: "direct-test control value"},
			{Usage: "--execute", Summary: "acknowledge actuation; omitted is a dry run"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"utility": {
		summary: "browse and run recovered utility plans",
		usage:   []string{"toyota utility list [ECU]", "toyota utility plan ITEM", "toyota utility run ECU ITEM"},
		examples: []terminal.HelpEntry{
			{Usage: "toyota utility plan allclear", Summary: "the recovered plan for a semantic utility"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--kind direct|routine|simple_operation", Summary: "run: disambiguate the plan"},
			{Usage: "--execute", Summary: "run: acknowledge; omitted is a dry run"},
			jsonOpt,
		}, liveLinkOpts...),
	},
}

// scan is an alias of health-check in the runtime parser.
func init() { liveDocs["scan"] = liveDocs["health-check"] }

// liveCommands are owned by the Python runtime and need a vehicle link.
var liveCommands = []string{
	"monitor", "observe", "scan", "health-check", "transport", "can", "did", "rid",
	"dtc", "uds", "ffd", "functional", "customize", "active-test", "utility",
}

func overview(p terminal.Printer) {
	p.Overview("Toyota diagnostics from the GTS+ catalog", []terminal.HelpGroup{
		{Title: "Explore", Entries: []terminal.HelpEntry{
			{Usage: "toyota", Summary: "open the explorer"},
			{Usage: "toyota search TERM…", Summary: "search signals, codes, tests, ECUs, vehicles"},
			{Usage: "toyota inspect ID", Summary: "open one search result by its #number"},
			{Usage: "toyota ECU", Summary: "one ECU at a glance, e.g. toyota frc"},
			{Usage: "toyota kinds / toyota ECU kinds", Summary: "list available record kinds"},
			{Usage: "toyota ECU KIND [QUERY]", Summary: "browse any kind; data, dtcs, tests also work"},
			{Usage: "toyota ECU IDENT", Summary: "decode one record, e.g. toyota frc 0x1601"},
			{Usage: "toyota ecu [QUERY]", Summary: "find ECUs"},
			{Usage: "toyota vehicle [NAME|TYPE]", Summary: "models, variants, install-set ECUs"},
		}},
		{Title: "Live", Entries: []terminal.HelpEntry{
			{Usage: "toyota monitor ECU ITEM…", Summary: "decoded Data List, live"},
			{Usage: "toyota scan", Summary: "all-system health check"},
			{Usage: "toyota dtc scan|clear", Summary: "read or clear trouble codes"},
			{Usage: "toyota vehicle detect", Summary: "identify the connected car"},
			{Usage: "toyota COMMAND --help", Summary: "did, rid, uds, can, ffd, observe, customize, …"},
		}},
	}, []string{
		"--region NA|EU|JP   --limit N   --json   --all   --db PATH   --version",
		"toyota help COMMAND for details · toyota completion zsh for tab completion",
	})
}

func commandHelp(p terminal.Printer, name string) bool {
	doc, ok := nativeDocs[name]
	if !ok {
		doc, ok = liveDocs[name]
	}
	if !ok {
		return false
	}
	p.CommandHelp(name, doc.summary, doc.usage, doc.examples, doc.options)
	return true
}

func ecuShorthandHelp(p terminal.Printer) {
	doc := nativeDocs["ecu"]
	p.CommandHelp("ECU", "work inside one ECU by key, name or category", doc.usage[1:], doc.examples[1:], doc.options)
}

// commandNames is every first word the CLI answers to, for suggestions.
func commandNames() []string {
	names := []string{"help"}
	for n := range nativeDocs {
		names = append(names, n)
	}
	names = append(names, liveCommands...)
	sort.Strings(names)
	return names
}

func usageLine(command string) string {
	if doc, ok := nativeDocs[command]; ok {
		for _, u := range doc.usage {
			if strings.HasPrefix(u, "toyota "+command) {
				return u
			}
		}
		if len(doc.usage) > 0 {
			return doc.usage[0]
		}
	}
	return "toyota help"
}
