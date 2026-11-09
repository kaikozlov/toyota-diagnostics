package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

type recorderShape struct{ integers, texts, booleans, ranges, references, relations string }

var recorderShapes = map[string]recorderShape{
	"ddr_signal":             {"bit_end bit_start did exception_flag exception_id name_string_index pattern_display_key physical_data_key presentation_mode signal_key signal_role sort_key visibility", "name", "list_member", "data_min data_max", "signal_info", "data_id_records invalid_condition_records"},
	"ddr_behavior_signal":    {"bit_end bit_start did exception_flag exception_id name_string_index pattern_display_key physical_data_key presentation_mode signal_key signal_role sort_key extraction_mode graph_max graph_min support_condition_key", "name family", "live_list_member", "data_min data_max", "signal_info", "invalid_condition_records"},
	"ddr_condition":          {"combination_mode comparison_mode comparison_value condition_order exception_flag exception_id referenced_signal_key signal_key", "comparison", "", "", "", ""},
	"ddr_behavior_condition": {"combination_mode comparison_mode condition_order exception_flag exception_id referenced_signal_key signal_key", "comparison family", "", "", "", ""},
	"ddr_data_id":            {"did exception_flag exception_id payload_length validity_mode", "", "", "", "", ""},
	"ddr_trigger":            {"dtc_identifier exception_flag exception_id name_string_index trigger_memo_string_index trigger_point_enabled trigger_selector_0 trigger_selector_1 unresolved_u16_0e", "name trigger_memo", "", "", "", ""},
	"ddr_behavior_code":      {"additional_string_index behavior_key dependent_data_0 dependent_data_1 exception_flag exception_id name_string_index trigger_memo_string_index trigger_selector_0 trigger_selector_1", "additional_text family name trigger_memo", "", "", "", ""},
}

func (d *DB) recorderRecord(ctx context.Context, kind string, id int64) (any, error) {
	shape, ok := recorderShapes[kind]
	if !ok || id <= 0 {
		return nil, fmt.Errorf("invalid recorder reference")
	}
	var definition, table, record int64
	var source string
	err := d.db.QueryRowContext(ctx, "SELECT e.definition_id,t.value,e.table_number,e.record_number FROM "+kind+"_entries e JOIN text_values t ON t.id=e.source_id WHERE e.id=?", id).Scan(&definition, &source, &table, &record)
	if err != nil {
		return nil, err
	}
	columns := []string{}
	fields := []string{}
	types := []byte{}
	add := func(names string, suffix string, tag byte) {
		for _, name := range strings.Fields(names) {
			columns = append(columns, name+suffix)
			fields = append(fields, name)
			types = append(types, tag)
		}
	}
	add(shape.integers, "", 'i')
	add(shape.booleans, "", 'b')
	add(shape.texts, "_id", 't')
	add(shape.ranges, "", 'f')
	add(shape.references, "_id", 'r')
	if kind == "ddr_behavior_condition" {
		columns = append(columns, "comparison_bits64")
		fields = append(fields, "comparison_bits64")
		types = append(types, 'u')
	}
	add(shape.relations, "_count", 'l')
	if kind == "ddr_condition" {
		columns = append(columns, "unknown_byte15")
		fields = append(fields, "unknown_byte15")
		types = append(types, 'x')
	}
	values := make([]any, len(columns))
	scan := make([]any, len(values))
	for i := range scan {
		scan[i] = &values[i]
	}
	if err = d.db.QueryRowContext(ctx, "SELECT "+strings.Join(columns, ",")+" FROM "+kind+"_defs WHERE id=?", definition).Scan(scan...); err != nil {
		return nil, err
	}
	out := map[string]any{"source": source, "table": signalNumber(table), "record": signalNumber(record)}
	for i, field := range fields {
		v := values[i]
		switch types[i] {
		case 'i', 'b', 'r', 'l':
			number, valid := v.(int64)
			if !valid {
				return nil, fmt.Errorf("invalid recorder integer %s", field)
			}
			switch types[i] {
			case 'i':
				out[field] = signalNumber(number)
			case 'b':
				if number != 0 && number != 1 {
					return nil, fmt.Errorf("invalid recorder boolean")
				}
				out[field] = number == 1
			case 'r':
				if number <= 0 {
					return nil, fmt.Errorf("invalid recorder decoder reference")
				}
				out[field] = map[string]any{"$signal_info": signalNumber(number)}
			case 'l':
				if number < 0 {
					return nil, fmt.Errorf("invalid recorder relation count")
				}
				rows, e := d.db.QueryContext(ctx, "SELECT position,value FROM "+kind+"_"+field+" WHERE definition_id=? ORDER BY position", definition)
				if e != nil {
					return nil, e
				}
				list := []any{}
				for rows.Next() {
					var pos, value int64
					if e = rows.Scan(&pos, &value); e != nil {
						rows.Close()
						return nil, e
					}
					if pos != int64(len(list)) {
						rows.Close()
						return nil, fmt.Errorf("invalid recorder relationship order")
					}
					list = append(list, signalNumber(value))
				}
				e = rows.Err()
				rows.Close()
				if e != nil {
					return nil, e
				}
				if int64(len(list)) != number {
					return nil, fmt.Errorf("incomplete recorder relationship")
				}
				out[field] = list
			}
		case 't':
			out[field] = nil
			if v != nil {
				number, valid := v.(int64)
				if !valid {
					return nil, fmt.Errorf("invalid recorder text reference")
				}
				var label string
				if err = d.db.QueryRowContext(ctx, "SELECT value FROM text_values WHERE id=?", number).Scan(&label); err != nil {
					return nil, err
				}
				out[field] = label
			}
		case 'f':
			number, valid := v.(float64)
			if !valid {
				return nil, fmt.Errorf("invalid recorder range")
			}
			out[field] = signalNumber(number)
		case 'x':
			if v != nil {
				hex, valid := v.(string)
				if !valid || len(hex) != 2 || strings.Trim(hex, "0123456789abcdef") != "" {
					return nil, fmt.Errorf("invalid recorder unknown byte")
				}
				out["uninterpreted_bytes"] = map[string]any{"15": hex}
			}
		case 'u':
			decimal, valid := v.(string)
			if !valid {
				return nil, fmt.Errorf("invalid recorder comparison bits")
			}
			bits, e := strconv.ParseUint(decimal, 10, 64)
			if e != nil || strconv.FormatUint(bits, 10) != decimal {
				return nil, fmt.Errorf("invalid recorder comparison bits")
			}
			out[field] = json.Number(decimal)
		}
	}
	return out, nil
}
