package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type nativeRecorderSpec struct {
	integers, texts, booleans, references, relation string
	pair                                            bool
}

var nativeRecorderUnknown = [][]string{{"13_15"}, {"15"}, {"8_11", "42_43", "46_47"}, {"33_35"}, {"9", "11"}, {"7"}}
var nativeRecorderNames = []string{"addresses", "invalid_conditions", "monitors", "triggers", "case_data_ids", "header_data_ids"}
var nativeRecorderSpecs = []nativeRecorderSpec{
	{"address exception_flag exception_id monitor_key record_length section_kind trigger_id", "family record_class", "header_member", "", "monitor_records", true},
	{"combination_mode comparison_mode comparison_value condition_order exception_flag exception_id referenced_signal_key signal_key", "comparison family record_class", "", "", "", false},
	{"bit_end bit_start data_max data_min exception_flag exception_id graph_max graph_min history_bit_end history_bit_start monitor_key name_string_index pattern_display_key physical_data_key presentation_mode sort_key", "family name record_class", "", "signal_info", "invalid_condition_records", true},
	{"dependent_data_0 dependent_data_1 exception_flag exception_id memo_string_index name_string_index trigger_id trigger_variant", "family memo name record_class", "", "", "header_parameters", false},
	{"case_did case_parameter case_selector exception_flag exception_id header_did", "family record_class", "", "", "", false},
	{"exception_flag exception_id header_did header_parameter", "family record_class", "", "", "", false},
}

func nativeRecorderDomain(name string) int {
	for i, k := range nativeRecorderNames {
		if k == name {
			return i
		}
	}
	return -1
}
func (d *DB) nativeRecorder(ctx context.Context, name string, id int64) (any, error) {
	index := nativeRecorderDomain(name)
	if index < 0 || id <= 0 {
		return nil, fmt.Errorf("invalid native recorder reference")
	}
	if value, ok := d.nativeRecorders[index].load(id); ok {
		return value, nil
	}
	found, err := d.nativeRecorderChunk(ctx, index, []int64{id})
	if err != nil {
		return nil, err
	}
	value := found[id]
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d.nativeRecorders[index].store(id, value, len(encoded))
	return value, nil
}
func (d *DB) nativeRecorderChunk(ctx context.Context, index int, ids []int64) (map[int64]any, error) {
	if len(ids) == 0 || len(ids) > 256 {
		return nil, fmt.Errorf("invalid native recorder batch")
	}
	spec := nativeRecorderSpecs[index]
	name := nativeRecorderNames[index]
	ints, texts, bools, refs := strings.Fields(spec.integers), strings.Fields(spec.texts), strings.Fields(spec.booleans), strings.Fields(spec.references)
	cols := []string{"e.id", "d.id", "source.value", "e.table_number", "e.record_number"}
	joins := []string{"JOIN native_recorder_" + name + "_defs d ON d.id=e.definition_id", "JOIN text_values source ON source.id=e.source_id"}
	for _, k := range ints {
		cols = append(cols, "d."+k)
	}
	for _, k := range bools {
		cols = append(cols, "d."+k)
	}
	for i, k := range texts {
		alias := fmt.Sprintf("t%d", i)
		cols = append(cols, "d."+k+"_id", alias+".value")
		joins = append(joins, "LEFT JOIN text_values "+alias+" ON "+alias+".id=d."+k+"_id")
	}
	for _, k := range refs {
		cols = append(cols, "d."+k+"_id")
	}
	cols = append(cols, "d.uninterpreted_present")
	for i, key := range nativeRecorderUnknown[index] {
		alias := fmt.Sprintf("u%d", i)
		column := "d.unknown_" + key + "_id"
		cols = append(cols, column, alias+".value")
		joins = append(joins, "LEFT JOIN text_values "+alias+" ON "+alias+".id="+column)
	}
	if spec.relation != "" {
		cols = append(cols, "d.relation_count")
	}
	size := 256
	if len(ids) == 1 {
		size = 1
	}
	args := make([]any, size)
	marks := make([]string, size)
	for i := range marks {
		marks[i] = "?"
	}
	for i, id := range ids {
		if id <= 0 {
			return nil, fmt.Errorf("invalid native recorder ID")
		}
		args[i] = id
	}
	suffix := strings.Join(marks, ",")
	rows, err := d.decodedQuery(ctx, fmt.Sprintf("native_%s_scalar_%d", name, size), "SELECT "+strings.Join(cols, ",")+" FROM native_recorder_"+name+"_entries e "+strings.Join(joins, " ")+" WHERE e.id IN("+suffix+")", args)
	if err != nil {
		return nil, err
	}
	found := map[int64]any{}
	owners := map[int64]int64{}
	counts := map[int64]int64{}
	vals := make([]any, len(cols))
	dest := make([]any, len(cols))
	for i := range dest {
		dest[i] = &vals[i]
	}
	for rows.Next() {
		if err = rows.Scan(dest...); err != nil {
			rows.Close()
			return nil, err
		}
		id, ok := vals[0].(int64)
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("invalid recorder entry")
		}
		def, ok := vals[1].(int64)
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("invalid recorder definition")
		}
		source, ok := vals[2].(string)
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("missing recorder source")
		}
		table, ok := vals[3].(int64)
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("invalid recorder table")
		}
		record, ok := vals[4].(int64)
		if !ok {
			rows.Close()
			return nil, fmt.Errorf("invalid recorder record")
		}
		out := map[string]any{"source": source, "table": signalNumber(table), "record": signalNumber(record)}
		pos := 5
		number := func() (int64, error) {
			v, ok := vals[pos].(int64)
			pos++
			if !ok {
				return 0, fmt.Errorf("invalid recorder integer")
			}
			return v, nil
		}
		for _, k := range ints {
			v, e := number()
			if e != nil {
				rows.Close()
				return nil, e
			}
			out[k] = signalNumber(v)
		}
		for _, k := range bools {
			v, e := number()
			if e != nil || v < 0 || v > 1 {
				rows.Close()
				return nil, fmt.Errorf("invalid recorder boolean")
			}
			out[k] = v == 1
		}
		for _, k := range texts {
			ident, label := vals[pos], vals[pos+1]
			pos += 2
			if ident != nil && label == nil {
				rows.Close()
				return nil, fmt.Errorf("missing recorder label")
			}
			out[k] = label
		}
		for _, k := range refs {
			v, e := number()
			if e != nil || v <= 0 {
				rows.Close()
				return nil, fmt.Errorf("invalid signal-info reference")
			}
			out[k] = map[string]any{"$signal_info": signalNumber(v)}
		}
		present, e := number()
		if e != nil || present < 0 || present > 1 {
			rows.Close()
			return nil, fmt.Errorf("invalid uninterpreted presence")
		}
		unknown := map[string]any{}
		for _, key := range nativeRecorderUnknown[index] {
			ident, label := vals[pos], vals[pos+1]
			pos += 2
			if present == 1 {
				if ident == nil || label == nil {
					rows.Close()
					return nil, fmt.Errorf("missing uninterpreted range")
				}
				unknown[key] = label
			} else if ident != nil {
				rows.Close()
				return nil, fmt.Errorf("unexpected uninterpreted range")
			}
		}
		if present == 1 {
			out["uninterpreted_bytes"] = unknown
		}
		if spec.relation != "" {
			v, e := number()
			if e != nil || v < 0 {
				rows.Close()
				return nil, fmt.Errorf("invalid recorder relation count")
			}
			counts[def] = v
		}
		found[id] = out
		owners[id] = def
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if found[id] == nil {
			return nil, fmt.Errorf("missing native recorder %s %d", name, id)
		}
	}
	if spec.relation != "" {
		relations := map[int64][]any{}
		definitions := []int64{}
		for _, def := range owners {
			if _, ok := relations[def]; !ok {
				relations[def] = []any{}
				definitions = append(definitions, def)
			}
		}
		args = make([]any, size)
		for i, id := range definitions {
			args[i] = id
		}
		fields := "value"
		if spec.pair {
			fields = "record,\"table\""
		}
		rows, err = d.decodedQuery(ctx, fmt.Sprintf("native_%s_links_%d", name, size), "SELECT definition_id,position,"+fields+" FROM native_recorder_"+name+"_links WHERE definition_id IN("+suffix+") ORDER BY definition_id,position", args)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var def, pos, value, table int64
			dest := []any{&def, &pos, &value}
			if spec.pair {
				dest = append(dest, &table)
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, err
			}
			list, ok := relations[def]
			if !ok || pos != int64(len(list)) {
				rows.Close()
				return nil, fmt.Errorf("invalid native recorder link order")
			}
			var v any = signalNumber(value)
			if spec.pair {
				v = map[string]any{"record": signalNumber(value), "table": signalNumber(table)}
			}
			relations[def] = append(list, v)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		for def, list := range relations {
			if int64(len(list)) != counts[def] {
				return nil, fmt.Errorf("incomplete native recorder links")
			}
		}
		for id, value := range found {
			value.(map[string]any)[spec.relation] = relations[owners[id]]
		}
	}
	return found, nil
}
func (d *DB) nativeRecorderArray(ctx context.Context, nodes []any, active referenceStack) ([]any, bool, error) {
	if len(nodes) < 4 {
		return nil, false, nil
	}
	type request struct {
		id  int64
		pos int
	}
	groups := map[int][]request{}
	for pos, node := range nodes {
		obj, ok := node.(map[string]any)
		if !ok {
			continue
		}
		kind, raw, ok := storedReference(obj)
		if !ok || !strings.HasPrefix(kind, "$native_recorder_") {
			continue
		}
		index := nativeRecorderDomain(strings.TrimPrefix(kind, "$native_recorder_"))
		if index < 0 {
			continue
		}
		id, err := referenceID(raw, kind)
		if err != nil {
			return nil, true, err
		}
		if active[referenceKey{kind, id}] {
			return nil, true, fmt.Errorf("cyclic native recorder reference")
		}
		groups[index] = append(groups[index], request{id, pos})
	}
	if len(groups) == 0 {
		return nil, false, nil
	}
	out := append([]any(nil), nodes...)
	for index, group := range groups {
		for start := 0; start < len(group); start += 256 {
			end := start + 256
			if end > len(group) {
				end = len(group)
			}
			ids := make([]int64, end-start)
			for i := range ids {
				ids[i] = group[start+i].id
			}
			found, err := d.nativeRecorderChunk(ctx, index, ids)
			if err != nil {
				return nil, true, err
			}
			for _, r := range group[start:end] {
				out[r.pos] = found[r.id]
			}
		}
	}
	return out, true, nil
}
