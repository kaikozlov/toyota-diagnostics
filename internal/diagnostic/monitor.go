package diagnostic

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

func FlattenSignalRecord(value map[string]any) []map[string]any {
	out := []map[string]any{}
	ecu := resolveMap(value["ecu"])["key"]
	for _, signal := range resolveRows(value["signals"]) {
		rendered := signal["pattern"]
		if rendered == nil {
			rendered = signal["value"]
		}
		out = append(out, map[string]any{"ecu": ecu, "did": value["did"], "signal": signal["name"], "value": rendered, "numeric_value": signal["value"], "pattern": signal["pattern"], "raw": signal["raw"], "unit": signal["unit"], "error": signal["error"]})
	}
	if len(out) == 0 {
		out = append(out, map[string]any{"ecu": ecu, "did": value["did"], "signal": "(raw)", "value": value["data_hex"]})
	}
	return out
}

type MonitorState map[string][]any

func RenderMonitorTable(ecu string, sample int, elapsed float64, values []map[string]any, changed bool, previous MonitorState) (string, MonitorState) {
	rows := []map[string]any{}
	state := MonitorState{}
	for _, value := range values {
		for _, row := range FlattenSignalRecord(value) {
			key := fmt.Sprintf("%v/%v/%v", row["ecu"], row["did"], row["signal"])
			current := []any{row["value"], row["raw"], row["error"]}
			state[key] = current
			if !changed || previous == nil || !reflect.DeepEqual(previous[key], current) {
				rows = append(rows, row)
			}
		}
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s · sample %d · +%.3fs", ecu, sample, elapsed)
	if len(rows) == 0 {
		return b.String() + "\n  (no changes)", state
	}
	width := 18
	ecus := map[string]bool{}
	ecuWidth := 5
	for _, r := range rows {
		s := resolveText(r["signal"])
		width = max(width, min(46, len([]rune(s))))
		e := resolveText(r["ecu"])
		ecus[e] = true
		ecuWidth = max(ecuWidth, min(16, len([]rune(e))))
	}
	b.WriteString("\n  ")
	if len(ecus) > 1 {
		fmt.Fprintf(&b, "%-*s ", ecuWidth, "ECU")
	}
	fmt.Fprintf(&b, "%-8s %-*s Value", "DID", width, "Signal")
	for _, row := range rows {
		did := "-"
		if row["did"] != nil {
			n, _ := resolveInt(row["did"])
			did = fmt.Sprintf("0x%04X", n)
		}
		value := resolveText(row["value"])
		if row["error"] != nil && resolveText(row["error"]) != "" {
			value = "ERR: " + resolveText(row["error"])
		}
		b.WriteString("\n  ")
		if len(ecus) > 1 {
			fmt.Fprintf(&b, "%-*s ", ecuWidth, resolveText(row["ecu"]))
		}
		fmt.Fprintf(&b, "%-8s %-*s %-22s %s", did, width, resolveText(row["signal"]), value, resolveText(row["unit"]))
	}
	return strings.TrimRight(b.String(), " "), state
}

type MonitorOptions struct {
	Interval                   time.Duration
	Count                      int
	Changed, JSONL, CSV, Clear bool
}

func RunMonitor(ctx context.Context, stream io.Writer, ecu string, read func(context.Context) ([]map[string]any, error), opts MonitorOptions) error {
	if opts.Interval < 0 {
		return fmt.Errorf("--interval must be >= 0")
	}
	if opts.Count < 0 {
		return fmt.Errorf("--count must be >= 0 (0 means until interrupted)")
	}
	if opts.JSONL && opts.CSV {
		return fmt.Errorf("--jsonl and --csv are mutually exclusive")
	}
	var csvWriter *csv.Writer
	fields := []string{"sample", "elapsed_s", "ecu", "did", "signal", "value", "numeric_value", "pattern", "raw", "unit", "error"}
	if opts.CSV {
		csvWriter = csv.NewWriter(stream)
		if e := csvWriter.Write(fields); e != nil {
			return e
		}
	}
	started := time.Now()
	var previous MonitorState
	for sample := 1; opts.Count == 0 || sample <= opts.Count; sample++ {
		if e := ctx.Err(); e != nil {
			return e
		}
		values, e := read(ctx)
		if e != nil {
			return e
		}
		elapsed := float64(time.Since(started).Microseconds()) / 1e6
		if opts.JSONL {
			e = json.NewEncoder(stream).Encode(map[string]any{"sample": sample, "elapsed_s": elapsed, "values": values})
		} else if csvWriter != nil {
			for _, value := range values {
				for _, row := range FlattenSignalRecord(value) {
					row["sample"] = sample
					row["elapsed_s"] = elapsed
					if row["did"] != nil {
						n, _ := resolveInt(row["did"])
						row["did"] = fmt.Sprintf("0x%04X", n)
					}
					cells := make([]string, len(fields))
					for i, f := range fields {
						cells[i] = resolveText(row[f])
					}
					if e = csvWriter.Write(cells); e != nil {
						return e
					}
				}
			}
			csvWriter.Flush()
			e = csvWriter.Error()
		} else {
			var text string
			text, previous = RenderMonitorTable(ecu, sample, elapsed, values, opts.Changed, previous)
			if opts.Clear {
				_, e = io.WriteString(stream, "\x1b[2J\x1b[H")
			}
			if e == nil {
				_, e = fmt.Fprintln(stream, text)
			}
		}
		if e != nil {
			return e
		}
		if opts.Count != 0 && sample == opts.Count {
			break
		}
		timer := time.NewTimer(opts.Interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	return nil
}
