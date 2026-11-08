package terminal

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

type record map[string]any

func decode(raw json.RawMessage) record {
	var r record
	if err := json.Unmarshal(raw, &r); err != nil {
		return nil
	}
	return r
}

func (r record) str(key string) string {
	if s, ok := r[key].(string); ok {
		return s
	}
	return ""
}

func (r record) num(key string) (int64, bool) {
	f, ok := r[key].(float64)
	if !ok || f != math.Trunc(f) {
		return 0, false
	}
	return int64(f), true
}

func (r record) flag(key string) (bool, bool) {
	b, ok := r[key].(bool)
	return b, ok
}

func (r record) pair(key string) (int64, int64, bool) {
	v, ok := r[key].([]any)
	if !ok || len(v) != 2 {
		return 0, 0, false
	}
	a, aok := v[0].(float64)
	b, bok := v[1].(float64)
	return int64(a), int64(b), aok && bok
}

func (r record) length(key string) int {
	if v, ok := r[key].([]any); ok {
		return len(v)
	}
	return 0
}

func hex(n int64, digits int) string { return fmt.Sprintf("0x%0*X", digits, n) }

// hexID parses an "0x…" identifier back to a number, or -1 when malformed.
func hexID(id string) int64 {
	n, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimPrefix(id, "0x"), "0X"), 16, 64)
	if err != nil {
		return -1
	}
	return n
}

// floatPair reads a two-element float range, keeping fractions (unlike pair).
func (r record) floatPair(key string) (float64, float64, bool) {
	v, ok := r[key].([]any)
	if !ok || len(v) != 2 {
		return 0, 0, false
	}
	a, aok := v[0].(float64)
	b, bok := v[1].(float64)
	return a, b, aok && bok
}

func fmtFloat(v float64) string { return strconv.FormatFloat(v, 'g', -1, 64) }

// Item prints one catalog record, decoded for its kind.
func (p Printer) Item(it catalog.ItemDetail) {
	p.title(p.bold(it.Name), p.kind(it.Kind)+p.dim(" · "+it.Region))
	var sub []string
	if it.ECU != "" {
		sub = append(sub, p.blue(it.ECU)+" "+p.dim(it.ECUName))
	}
	if it.Identifier != "" && it.Identifier != it.Name {
		sub = append(sub, p.dim(it.Identifier))
	}
	if len(sub) > 0 {
		p.println(strings.Join(sub, p.dim("  ·  ")))
	}
	p.println()
	rec := decode(it.Payload)
	switch {
	case rec == nil:
		p.println("  " + p.dim("Payload is not a JSON object; use --json for the raw record."))
	case it.Kind == "did":
		p.signal(it, rec)
	case it.Kind == "dtc":
		p.dtc(it, rec)
	case it.Kind == "active-test" || it.Kind == "utility":
		p.test(it, rec)
	default:
		p.generic(rec, map[string]bool{"name": true})
	}
	// The record number is information, not a hint, so it survives NoHints.
	if p.NoHints {
		p.hintLines(fmt.Sprintf("record #%d", it.ID))
		return
	}
	p.footer(fmt.Sprintf("record #%d", it.ID), "--json prints the complete source record")
}

func bits(start, end int64) string {
	if start == end {
		return fmt.Sprintf("bit %d", start)
	}
	return fmt.Sprintf("bits %d–%d", start, end)
}

// scaling describes how a raw field becomes the displayed value, mirroring
// the P5 linear decoder: raw × mul, ÷ div when div > 1, then + offset.
func scaling(r record) string {
	mul, _ := r.num("mul")
	div, _ := r.num("div")
	off, _ := r.num("offset")
	dec, _ := r.num("decimal_point_count")
	expr := "raw"
	if mul != 1 && mul != 0 {
		expr += fmt.Sprintf(" × %d", mul)
	}
	if div > 1 {
		expr += fmt.Sprintf(" ÷ %d", div)
	}
	if off > 0 {
		expr += fmt.Sprintf(" + %d", off)
	} else if off < 0 {
		expr += fmt.Sprintf(" − %d", -off)
	}
	if dec > 0 {
		expr += fmt.Sprintf(", shown with %d decimals", dec)
	}
	if expr == "raw" {
		return "raw value as is"
	}
	return expr
}

func sortedPatterns(m map[string]any) [][2]string {
	out := make([][2]string, 0, len(m))
	for k, v := range m {
		out = append(out, [2]string{k, fmt.Sprint(v)})
	}
	sort.Slice(out, func(i, j int) bool {
		a, aerr := strconv.Atoi(out[i][0])
		b, berr := strconv.Atoi(out[j][0])
		if aerr == nil && berr == nil {
			return a < b
		}
		return out[i][0] < out[j][0]
	})
	return out
}

// Known decoder families get a fully decoded page: every payload field is
// either rendered below or is generator bookkeeping kept for --json. Any
// other decoder keeps the universal rows and surfaces its remaining fields
// generically, uninterpreted.
const (
	p5Decoder = "p5-linear-msb0-v1"
	p6Decoder = "p6-native-msb0-u32-v1"
)

func (p Printer) signal(it catalog.ItemDetail, r record) {
	decoder := r.str("decoder")
	known := decoder == p5Decoder || decoder == p6Decoder
	// The alternate DID is only news when it is a different number: P6
	// repeats the primary DID in the alternate slot.
	if alt, ok := r.num("alternate_did"); ok && alt != hexID(it.Identifier) {
		p.kv("DID", it.Identifier+p.dim("   alternate "+hex(alt, 4)))
	} else {
		p.kv("DID", it.Identifier)
	}
	if s, ok := r.num("bit_start"); ok {
		if e, ok := r.num("bit_end"); ok {
			p.kv("Field", fmt.Sprintf("%s  %s", bits(s, e), p.dim(fmt.Sprintf("%d wide, MSB-first", e-s+1))))
		}
	}
	if decoder == p5Decoder {
		if v := scaling(r); v != "raw value as is" {
			p.kv("Value", v)
		}
		if signed, _ := r.flag("signed"); signed {
			p.kv("Signed", "two's complement")
		}
	}
	if decoder == p6Decoder {
		// The sign_mode enum is generator vocabulary; report the raw value
		// instead of guessing a label. Zero is the common unsigned case.
		if m, ok := r.num("sign_mode"); ok && m != 0 {
			p.kv("Sign mode", strconv.FormatInt(m, 10))
		}
	}
	if u := r.str("unit"); u != "" {
		p.kv("Unit", u)
	}
	// A range that ends before it starts carries no usable information.
	// P6 reports physical values directly; its graph_range is the same
	// range in pre-decimal-point counts.
	if lo, hi, ok := r.floatPair("physical_range"); ok && hi >= lo {
		p.kv("Range", fmtFloat(lo)+" … "+fmtFloat(hi))
	} else if lo, hi, ok := r.pair("data_range"); ok && hi >= lo {
		p.kv("Range", fmt.Sprintf("%d … %d", lo, hi))
	}
	if decoder != p6Decoder {
		if lo, hi, ok := r.pair("graph_range"); ok && hi >= lo {
			if dlo, dhi, ok := r.pair("data_range"); !ok || dlo != lo || dhi != hi {
				p.kv("Graph", fmt.Sprintf("%d … %d", lo, hi))
			}
		}
	}
	if d := r.str("decoder"); d != "" {
		p.kv("Decoder", p.dim(d))
	}
	if !known {
		p.generic(r, map[string]bool{
			"name": true, "decoder": true, "alternate_did": true,
			"bit_start": true, "bit_end": true, "unit": true,
			"data_range": true, "graph_range": true, "patterns": true,
			"pattern_display": true, "physical_range": true,
		})
	}
	patterns, _ := r["patterns"].(map[string]any)
	if len(patterns) == 0 {
		patterns, _ = r["pattern_display"].(map[string]any)
	}
	if len(patterns) > 0 {
		p.println()
		p.section("Values", plural(len(patterns), "state", "states"))
		rows := sortedPatterns(patterns)
		keyW := 0
		for _, row := range rows {
			keyW = max(keyW, len(row[0]))
		}
		for _, row := range rows {
			p.println("  " + padLeft(p.blue(row[0]), keyW) + "  " + oneLine(row[1]))
		}
	}
}

var systemOf = map[byte]string{'P': "Powertrain", 'C': "Chassis", 'B': "Body", 'U': "Network"}

func (p Printer) dtc(it catalog.ItemDetail, r record) {
	code := it.Identifier
	if len(code) == 7 {
		p.kv("Code", p.bold(code[:5]))
		failure := r.str("failure")
		if failure == "" {
			failure = it.Detail
		}
		ftb := code[5:]
		if failure != "" {
			ftb += "  " + p.dim(failure)
		}
		p.kv("Failure byte", ftb)
	} else {
		p.kv("Code", p.bold(code))
		p.kv("Failure", firstNonEmpty(r.str("failure"), it.Detail))
	}
	if code != "" {
		if sys, ok := systemOf[code[0]]; ok {
			p.kv("System", sys)
		}
	}
	if d := r.str("description"); d != "" && d != it.Name {
		p.kv("Description", d)
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func (p Printer) test(it catalog.ItemDetail, r record) {
	p.kv("Identifier", it.Identifier)
	if k := r.str("kind"); k != "" {
		p.kv("Kind", k)
	}
	if e := r.str("execution"); e != "" {
		p.kv("Execution", strings.ReplaceAll(e, "_", " "))
	}
	if did, ok := r.num("did"); ok {
		p.kv("DID", hex(did, 4))
	}
	if rid, ok := r.num("routine_id"); ok {
		p.kv("Routine", hex(rid, 4))
	}
	if resp, ok := r.num("positive_response"); ok {
		p.kv("Response", hex(resp, 2))
	}
	if s := r.str("session_requirement"); s != "" {
		p.kv("Session", s)
	}
	if n, ok := r.num("runtime_length_minimum"); ok {
		p.kv("Min. length", fmt.Sprintf("%d bytes", n))
	}
	if m := r.str("monitor_name"); m != "" {
		p.kv("Monitor", m)
	}
	start, stop := r.length("start_frames"), r.length("stop_frames")
	if start+stop > 0 {
		p.kv("Sequence", fmt.Sprintf("%s · %s", plural(start, "start frame", "start frames"), plural(stop, "stop frame", "stop frames")))
	}
	if b := r.str("boundary"); b != "" {
		p.println()
		p.println("  " + p.dim(b))
	}
}

// generic summarises any record: scalars inline, structures by size. Nothing
// is hidden permanently; --json carries the whole payload.
func (p Printer) generic(r record, skip map[string]bool) {
	keys := make([]string, 0, len(r))
	for k := range r {
		if !skip[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		label := strings.ReplaceAll(k, "_", " ")
		label = strings.ToUpper(label[:1]) + label[1:]
		switch v := r[k].(type) {
		case nil:
		case string:
			if v != "" {
				p.kv(fit(label, labelWidth-1), fit(v, 160))
			}
		case float64:
			if v == math.Trunc(v) {
				p.kv(fit(label, labelWidth-1), strconv.FormatInt(int64(v), 10))
			} else {
				p.kv(fit(label, labelWidth-1), strconv.FormatFloat(v, 'g', -1, 64))
			}
		case bool:
			p.kv(fit(label, labelWidth-1), strconv.FormatBool(v))
		case []any:
			if len(v) == 0 {
				continue
			}
			if inline, ok := scalarList(v); ok {
				p.kv(fit(label, labelWidth-1), fit(inline, 160))
			} else {
				p.kv(fit(label, labelWidth-1), p.dim(plural(len(v), "entry", "entries")))
			}
		case map[string]any:
			if len(v) > 0 {
				p.kv(fit(label, labelWidth-1), p.dim(plural(len(v), "field", "fields")))
			}
		}
	}
}

func scalarList(v []any) (string, bool) {
	parts := make([]string, 0, len(v))
	for _, e := range v {
		switch x := e.(type) {
		case string:
			parts = append(parts, x)
		case float64:
			parts = append(parts, strconv.FormatFloat(x, 'f', -1, 64))
		default:
			return "", false
		}
	}
	return strings.Join(parts, " · "), true
}
