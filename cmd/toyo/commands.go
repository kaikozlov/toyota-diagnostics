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

var regionOpt = terminal.HelpEntry{Usage: "--region NA|EU|JP", Summary: "GTS+ region (default NA, or $TOYO_REGION)"}
var jsonOpt = terminal.HelpEntry{Usage: "--json", Summary: "structured output for scripts"}
var limitOpt = terminal.HelpEntry{Usage: "--limit N", Summary: "rows to show (default 50)"}

// nativeDocs describes the offline commands. Names here are also the
// completion and did-you-mean vocabulary.
var nativeDocs = map[string]commandDoc{
	"search": {
		summary: "search every signal, code, test, ECU and vehicle",
		usage:   []string{"toyo search TERM…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo search LTA", Summary: "everything Toyota calls LTA"},
			{Usage: "toyo search 1601", Summary: "the same DID as 0x1601"},
			{Usage: "toyo search steering angle", Summary: "all words must match"},
			{Usage: "toyo frc LTA", Summary: "the same, inside one ECU"},
		},
		options: []terminal.HelpEntry{regionOpt, limitOpt, jsonOpt},
	},
	"inspect": {
		summary: "show one record by its search-result number",
		usage:   []string{"toyo inspect ID", "toyo inspect ECU IDENT|TEXT…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo inspect 161299", Summary: "the number at the end of a search row"},
			{Usage: "toyo inspect frc 0x1601", Summary: "same as toyo frc 0x1601"},
			{Usage: "toyo inspect frc LTA control", Summary: "same as toyo frc LTA control"},
		},
		options: []terminal.HelpEntry{jsonOpt},
	},
	"ecu": {
		summary: "list ECUs, or work inside one",
		usage: []string{
			"toyo ecu [QUERY]",
			"toyo ECU",
			"toyo ECU data|dtcs|tests|functions|plugins|utilities [QUERY]",
			"toyo ECU IDENT",
			"toyo ECU TEXT…",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyo ecu brake", Summary: "ECUs whose key or name has \"brake\""},
			{Usage: "toyo frc", Summary: "one ECU at a glance (key, name or category)"},
			{Usage: "toyo frc data steer", Summary: "its Data List, filtered"},
			{Usage: "toyo frc dtcs U0131", Summary: "its trouble codes"},
			{Usage: "toyo frc 0x1601", Summary: "decode one DID, DTC or Active Test"},
			{Usage: "toyo frc LTA control", Summary: "search just this ECU"},
		},
		options: []terminal.HelpEntry{
			{Usage: "--all", Summary: "include ECUs that have no decoded catalog"},
			regionOpt, limitOpt, jsonOpt,
		},
	},
	"vehicle": {
		summary: "browse Toyota vehicles and their declared ECUs",
		usage:   []string{"toyo vehicle [NAME|TYPE]", "toyo vehicle detect|mounted"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo vehicle", Summary: "every model, grouped"},
			{Usage: "toyo vehicle Camry", Summary: "its variants"},
			{Usage: "toyo vehicle 10041", Summary: "one type and its install-set ECUs"},
			{Usage: "toyo vehicle detect", Summary: "live: identify the connected car"},
		},
		options: []terminal.HelpEntry{regionOpt, limitOpt, jsonOpt},
	},
	"browse": {
		summary: "open the interactive explorer",
		usage:   []string{"toyo", "toyo browse"},
		options: []terminal.HelpEntry{regionOpt},
	},
	"db": {
		summary: "inspect the offline catalog",
		usage:   []string{"toyo db [status]", "toyo db verify"},
		options: []terminal.HelpEntry{
			{Usage: "--db PATH", Summary: "use another catalog (or $TOYO_CATALOG)"},
			jsonOpt,
		},
	},
	"completion": {
		summary: "shell completion that knows your ECUs",
		usage:   []string{"toyo completion zsh|bash|fish"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo completion zsh > ~/.zfunc/_toyo", Summary: "zsh (add ~/.zfunc to fpath)"},
			{Usage: "source <(toyo completion bash)", Summary: "bash"},
			{Usage: "toyo completion fish > ~/.config/fish/completions/toyo.fish", Summary: "fish"},
		},
	},
	"kinds": {
		summary: "list the record kinds present in the catalog",
		usage:   []string{"toyo kinds", "toyo ECU kinds"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo kinds", Summary: "every kind in this region"},
			{Usage: "toyo frc kinds", Summary: "kinds this ECU actually has"},
		},
		options: []terminal.HelpEntry{regionOpt, jsonOpt},
	},
}

var liveLinkOpts = []terminal.HelpEntry{
	{Usage: "--vehicle TYPE|NAME", Summary: "vehicle scope; live commands resolve it from the VIN when omitted"},
	{Usage: "--transport panda|j2534", Summary: "vehicle link backend (default panda)"},
	{Usage: "--j2534-library PATH", Summary: "J2534 provider library, discovered when omitted"},
	{Usage: "--j2534-device NAME", Summary: "J2534 device selector, or $TOYO_J2534_DEVICE"},
	{Usage: "--j2534-baud N", Summary: "J2534 CAN bitrate (default 500000)"},
	{Usage: "--obd-multiplexing", Summary: "direct Panda: remap logical bus 1 onto the OBD-II pins"},
	regionOpt,
}

var monitorOutputOpts = terminal.HelpEntry{Usage: "--jsonl | --csv", Summary: "one JSON object per sample group, or one CSV row per signal"}

// liveDocs describes vehicle-link commands. The same table answers
// `toyo help COMMAND` and usage errors for those verbs.
var liveDocs = map[string]commandDoc{
	"monitor": {
		summary: "watch decoded Data List values, live",
		usage:   []string{"toyo monitor ECU ITEM…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo monitor engine BATT", Summary: "every Data List signal Toyota calls BATT"},
			{Usage: "toyo monitor frc 0x1014 steer", Summary: "mix DID numbers and names"},
			{Usage: "toyo monitor frc data --interval 1", Summary: "whole Data List, once a second"},
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
		usage:   []string{"toyo observe ECU:ITEM…", "toyo observe PRESET"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo observe engine:BATT frc:steer", Summary: "any ECUs, DID numbers or terms"},
			{Usage: "toyo observe tss3-longitudinal", Summary: "a recovered preset"},
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
		usage:   []string{"toyo scan", "toyo health-check"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo scan", Summary: "mount states, DTCs, freeze frames, RoBs, identities"},
			{Usage: "toyo health-check --out run.json", Summary: "also save the full snapshot"},
			{Usage: "toyo health-check --compare run.json", Summary: "diff against a saved run"},
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
		usage:   []string{"toyo transport status", "toyo transport list"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo transport status", Summary: "backend, readiness and detail"},
			{Usage: "toyo transport list", Summary: "backends and discoverable J2534 providers"},
		},
		options: []terminal.HelpEntry{
			{Usage: "--json", Summary: "structured output for scripts"},
			{Usage: "--transport panda|j2534", Summary: "backend to report on (default panda)"},
			{Usage: "--j2534-library PATH", Summary: "J2534 provider library, discovered when omitted"},
			{Usage: "--j2534-device NAME", Summary: "J2534 device selector, or $TOYO_J2534_DEVICE"},
			{Usage: "--j2534-baud N", Summary: "J2534 CAN bitrate (default 500000)"},
			{Usage: "--obd-multiplexing", Summary: "direct Panda: remap logical bus 1 onto the OBD-II pins"},
		},
	},
	"can": {
		summary: "GTS CAN topology, or capture raw frames",
		usage:   []string{"toyo can topology", "toyo can sniff ADDRESS…"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo can topology", Summary: "the registry's bus map"},
			{Usage: "toyo can sniff 0x7E0", Summary: "capture one address"},
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
			"toyo did read ECU DID…",
			"toyo did watch ECU DID…",
			"toyo did support ECU [DID…]",
			"toyo did decode ECU DID PAYLOAD",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyo did read engine 0x1002", Summary: "one live read, decoded"},
			{Usage: "toyo did watch frc 0x1014 --interval 1", Summary: "repeat and watch"},
			{Usage: "toyo did decode frc 0x1202 01", Summary: "decode hex bytes offline"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--interval S", Summary: "watch seconds between samples (default 0.25)"},
			{Usage: "--count N", Summary: "watch samples; 0 runs until interrupted"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"rid": {
		summary: "live routine-identifier capability queries",
		usage:   []string{"toyo rid support ECU [RID…]"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo rid support engine", Summary: "every advertised RID"},
		},
		options: append([]terminal.HelpEntry{jsonOpt}, liveLinkOpts...),
	},
	"dtc": {
		summary: "scan or clear trouble codes; decode a status byte",
		usage:   []string{"toyo dtc scan", "toyo dtc clear", "toyo dtc decode STATUS"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo dtc scan", Summary: "every responding ECU, decoded descriptions"},
			{Usage: "toyo dtc scan --ecu engine", Summary: "one ECU (repeatable)"},
			{Usage: "toyo dtc decode 0x2F", Summary: "explain a status byte offline"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--all", Summary: "scan: include non-fault-status records"},
			{Usage: "--ecu KEY", Summary: "scan: limit to one ECU (repeatable)"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"uds": {
		summary: "send one raw UDS request",
		usage:   []string{"toyo uds raw ECU SERVICE [DATA]"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo uds raw frc 0x22 0x1202", Summary: "one request, response printed as hex"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--force", Summary: "acknowledge a mutating request"},
			{Usage: "--sub-address BYTE", Summary: "ISO-TP address-extension bytes"},
		}, liveLinkOpts...),
	},
	"ffd": {
		summary: "TSS3 freeze frames: browse definitions, read Operation/Image FFD",
		usage: []string{
			"toyo ffd data|robs [QUERY]",
			"toyo ffd operation list|records|read …",
			"toyo ffd image info|list|read …",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyo ffd data steer", Summary: "Operation-FFD signal definitions"},
			{Usage: "toyo ffd operation read 0x9D 1", Summary: "one stored Operation record, decoded"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--query TERM", Summary: "read: only blocks matching a DID/name"},
			jsonOpt,
		}, liveLinkOpts...),
	},
	"functional": {
		summary: "broadcast OBD-mode requests to all ECUs",
		usage:   []string{"toyo functional obd MODE [PAYLOAD]"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo functional obd 09 02", Summary: "ask every ECU for its VIN"},
		},
		options: append([]terminal.HelpEntry{
			{Usage: "--window S", Summary: "seconds to listen (default 1)"},
			{Usage: "--force", Summary: "acknowledge a mutating request"},
		}, liveLinkOpts...),
	},
	"customize": {
		summary: "browse and change OEM Customize settings",
		usage: []string{
			"toyo customize groups [QUERY]",
			"toyo customize items|info GROUP [ITEM]",
			"toyo customize read GROUP ITEM",
			"toyo customize set GROUP ITEM VALUE",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyo customize groups lamp", Summary: "find a group"},
			{Usage: "toyo customize read body lamp_turn_timing", Summary: "one live setting"},
			{Usage: "toyo customize set body lamp_turn_timing short", Summary: "dry-run plan; --execute writes"},
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
			"toyo active-test list [ECU]",
			"toyo active-test plan ECU ITEM",
			"toyo active-test run|stop ECU ITEM",
			"toyo active-test groups|group-run|group-stop …",
		},
		examples: []terminal.HelpEntry{
			{Usage: "toyo active-test list frc", Summary: "one ECU's tests"},
			{Usage: "toyo active-test plan frc 0xA429", Summary: "the zero-transmit plan"},
			{Usage: "toyo active-test run frc 0xA429", Summary: "dry run; --execute actuates"},
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
		usage:   []string{"toyo utility list [ECU]", "toyo utility plan ITEM", "toyo utility run ECU ITEM"},
		examples: []terminal.HelpEntry{
			{Usage: "toyo utility plan allclear", Summary: "the recovered plan for a semantic utility"},
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

// liveCommands need a vehicle link and are handled by the Go runtime.
var liveCommands = []string{
	"monitor", "observe", "scan", "health-check", "transport", "can", "did", "rid",
	"dtc", "uds", "ffd", "functional", "customize", "active-test", "utility",
}

func overview(p terminal.Printer) {
	p.Overview("Toyota diagnostics from the GTS+ catalog", []terminal.HelpGroup{
		{Title: "Explore", Entries: []terminal.HelpEntry{
			{Usage: "toyo", Summary: "open the explorer"},
			{Usage: "toyo search TERM…", Summary: "search signals, codes, tests, ECUs, vehicles"},
			{Usage: "toyo inspect ID", Summary: "open one search result by its #number"},
			{Usage: "toyo ECU", Summary: "one ECU at a glance, e.g. toyo frc"},
			{Usage: "toyo kinds / toyo ECU kinds", Summary: "list available record kinds"},
			{Usage: "toyo ECU KIND [QUERY]", Summary: "browse any kind; data, dtcs, tests also work"},
			{Usage: "toyo ECU IDENT", Summary: "decode one record, e.g. toyo frc 0x1601"},
			{Usage: "toyo ecu [QUERY]", Summary: "find ECUs"},
			{Usage: "toyo vehicle [NAME|TYPE]", Summary: "models, variants, install-set ECUs"},
		}},
		{Title: "Live", Entries: []terminal.HelpEntry{
			{Usage: "toyo monitor ECU ITEM…", Summary: "decoded Data List, live"},
			{Usage: "toyo scan", Summary: "all-system health check"},
			{Usage: "toyo dtc scan|clear", Summary: "read or clear trouble codes"},
			{Usage: "toyo vehicle detect", Summary: "identify the connected car"},
			{Usage: "toyo COMMAND --help", Summary: "did, rid, uds, can, ffd, observe, customize, …"},
		}},
	}, []string{
		"--region NA|EU|JP   --limit N   --json   --all   --db PATH   --version",
		"toyo help COMMAND for details · toyo completion zsh for tab completion",
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

func commandDocFor(command string) (commandDoc, bool) {
	if doc, ok := nativeDocs[command]; ok {
		return doc, true
	}
	doc, ok := liveDocs[command]
	return doc, ok
}

func usageLine(command string) string {
	if doc, ok := commandDocFor(command); ok {
		for _, u := range doc.usage {
			if strings.HasPrefix(u, "toyo "+command) {
				return u
			}
		}
		if len(doc.usage) > 0 {
			return doc.usage[0]
		}
	}
	return "toyo help"
}
