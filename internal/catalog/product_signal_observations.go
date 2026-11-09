package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

var observationIntegers = []string{"bit_end", "bit_start", "local_support_mode", "pattern_display_key", "physical_data_key", "sort_key", "did", "exception_handler_flag", "exception_handler_id", "extraction_mode", "presentation_mode", "record_key", "support_condition_key", "name_string_index", "flags", "monitor_key", "primary_did", "snapshot_did", "alternate_did", "visibility_override"}
var observationExtra = map[int64][]string{
	1: {"did", "dynamic_lsb_possible", "exception_handler_flag", "exception_handler_id", "extraction_mode", "presentation_mode", "record_key", "support_condition_key"},
	2: {"decoder", "did", "did_support", "exception_handler_flag", "exception_handler_id", "extraction_mode", "name_string_index", "physical_range", "presentation_mode", "record_key", "support_condition", "support_condition_key"},
	3: {"dynamic_lsb_possible", "flags", "monitor_key", "primary_did", "snapshot_did", "support_condition", "tables"},
	4: {"dynamic_lsb_possible", "flags", "monitor_key", "primary_did", "snapshot_did", "support_condition"},
	5: {"alternate_did", "decoder", "did_support", "extraction_mode", "flags", "graph_range", "monitor_key", "physical_range", "primary_did", "snapshot_did", "support_condition", "visibility_override"},
}

func (d *DB) signalObservation(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid signal observation")
	}
	if value, ok := d.signalObservations.load(id); ok {
		return value, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT d.*,n.value AS name_value,x.value AS decoder_value,s.value AS source_value,
 e.source_id,e.table_number,e.record_number FROM signal_observation_entries e
 JOIN signal_observations d ON d.id=e.definition_id
 LEFT JOIN text_values n ON n.id=d.name_id LEFT JOIN text_values x ON x.id=d.decoder_id
 LEFT JOIN text_values s ON s.id=e.source_id WHERE e.id=?`, id)
	if err != nil {
		return nil, err
	}
	columns, err := rows.Columns()
	if err != nil {
		rows.Close()
		return nil, err
	}
	if !rows.Next() {
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("missing signal observation %d", id)
	}
	values := make([]any, len(columns))
	dest := make([]any, len(columns))
	for i := range dest {
		dest[i] = &values[i]
	}
	err = rows.Scan(dest...)
	rows.Close()
	if err != nil {
		return nil, err
	}
	data := map[string]any{}
	for i, k := range columns {
		data[k] = values[i]
	}
	shape, ok := data["shape"].(int64)
	extra, known := observationExtra[shape]
	if !ok || !known {
		return nil, fmt.Errorf("invalid observation shape")
	}
	fields := map[string]bool{}
	for _, k := range []string{"bit_end", "bit_start", "local_support_mode", "name", "pattern_display_key", "physical_data_key", "record", "signal_info", "sort_key", "source", "table"} {
		fields[k] = true
	}
	for _, k := range extra {
		fields[k] = true
	}
	text := func(idColumn, valueColumn string) (any, error) {
		ref, value := data[idColumn], data[valueColumn]
		if (ref == nil) != (value == nil) {
			return nil, fmt.Errorf("missing observation text")
		}
		if ref != nil {
			if n, ok := ref.(int64); !ok || n <= 0 {
				return nil, fmt.Errorf("invalid observation text reference")
			}
			if _, ok := value.(string); !ok {
				return nil, fmt.Errorf("invalid observation text")
			}
		}
		return value, nil
	}
	name, err := text("name_id", "name_value")
	if err != nil {
		return nil, err
	}
	source, err := text("source_id", "source_value")
	if err != nil {
		return nil, err
	}
	out := map[string]any{"name": name, "source": source}
	for key, column := range map[string]string{"table": "table_number", "record": "record_number"} {
		v, ok := data[column].(int64)
		if !ok || v < 0 {
			return nil, fmt.Errorf("invalid observation origin")
		}
		out[key] = signalNumber(v)
	}
	for _, key := range observationIntegers {
		value := data[key]
		if fields[key] {
			if value == nil && key == "local_support_mode" {
				out[key] = nil
				continue
			}
			number, ok := value.(int64)
			if !ok {
				return nil, fmt.Errorf("missing observation integer %s", key)
			}
			out[key] = signalNumber(number)
		} else if value != nil {
			return nil, fmt.Errorf("unexpected observation integer %s", key)
		}
	}
	if fields["decoder"] {
		value, err := text("decoder_id", "decoder_value")
		if err != nil {
			return nil, err
		}
		out["decoder"] = value
	} else if data["decoder_id"] != nil {
		return nil, fmt.Errorf("unexpected observation decoder")
	}
	for _, ref := range []struct{ field, column, marker string }{{"signal_info", "info_id", "$signal_info"}, {"did_support", "support_id", "$did_support"}, {"support_condition", "condition_id", "$support_condition"}} {
		value := data[ref.column]
		if fields[ref.field] {
			if value == nil {
				if ref.field == "signal_info" {
					return nil, fmt.Errorf("missing signal info")
				}
				out[ref.field] = nil
			} else {
				n, ok := value.(int64)
				if !ok || n <= 0 {
					return nil, fmt.Errorf("invalid observation reference")
				}
				out[ref.field] = map[string]any{ref.marker: signalNumber(n)}
			}
		} else if value != nil {
			return nil, fmt.Errorf("unexpected observation reference")
		}
	}
	if fields["dynamic_lsb_possible"] {
		value, ok := data["dynamic_lsb"].(int64)
		if !ok || (value != 0 && value != 1) {
			return nil, fmt.Errorf("invalid dynamic LSB flag")
		}
		out["dynamic_lsb_possible"] = value == 1
	} else if data["dynamic_lsb"] != nil {
		return nil, fmt.Errorf("unexpected dynamic LSB flag")
	}
	for _, r := range []struct {
		field, low, high string
		real             bool
	}{{"physical_range", "physical_low", "physical_high", true}, {"graph_range", "graph_low", "graph_high", false}} {
		a, b := data[r.low], data[r.high]
		if fields[r.field] {
			if r.real {
				for _, v := range []any{a, b} {
					x, ok := v.(float64)
					if !ok || math.IsNaN(x) || math.IsInf(x, 0) {
						return nil, fmt.Errorf("invalid physical range")
					}
				}
			} else {
				if _, ok := a.(int64); !ok {
					return nil, fmt.Errorf("invalid graph range")
				}
				if _, ok := b.(int64); !ok {
					return nil, fmt.Errorf("invalid graph range")
				}
			}
			out[r.field] = []any{signalNumber(a), signalNumber(b)}
		} else if a != nil || b != nil {
			return nil, fmt.Errorf("unexpected observation range")
		}
	}
	if shape == 3 {
		count, ok := data["auxiliary_tables"].(int64)
		if !ok || count < 1 || count > 1000 {
			return nil, fmt.Errorf("invalid auxiliary tables")
		}
		tables := []any{signalNumber(int64(62))}
		for i := int64(0); i < count; i++ {
			tables = append(tables, signalNumber(int64(157)))
		}
		out["tables"] = tables
	} else if data["auxiliary_tables"] != nil {
		return nil, fmt.Errorf("unexpected auxiliary tables")
	}
	if data["unknown_bytes"] != nil && shape != 2 {
		return nil, fmt.Errorf("unexpected observation unknown bytes")
	}
	if err := restoreROBUnknown(out, data["unknown_bytes"], data["table_number"].(int64)); err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	d.signalObservations.store(id, out, len(encoded))
	return out, nil
}
