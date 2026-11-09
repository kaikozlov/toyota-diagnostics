package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

type planField struct{ name, kind string }
type planSchema struct {
	fields   []planField
	optional []string
	masks    []int64
}

func planFields(kind, names string) []planField {
	out := []planField{}
	for _, n := range strings.Fields(names) {
		out = append(out, planField{n, kind})
	}
	return out
}
func planJoin(groups ...[]planField) []planField {
	out := []planField{}
	for _, g := range groups {
		out = append(out, g...)
	}
	return out
}

// Closed diagnostic concepts. No database-supplied schemas or arbitrary JSON keys.
var operationPlanSchemas = map[string]planSchema{
	"variable":             {fields: []planField{{"bytes", "x"}, {"id", "s"}, {"normalized_id", "s"}}},
	"formula_frame":        {fields: []planField{{"base_frame", "$operation_base_frame"}, {"formula", "s"}, {"materialized_prefix", "x"}}},
	"static_frame":         {fields: []planField{{"base_frame", "$operation_base_frame"}, {"materialized_static_request", "x"}, {"static_command_variable", "~@variable"}}},
	"probe_frame":          {fields: []planField{{"base_frame", "$operation_base_frame"}, {"materialized_request", "x"}, {"positive_check", "x"}}},
	"length_probe":         {fields: planJoin(planFields("s", "decoded_value_formula kind received_length_formula selector"), []planField{{"response_prefix_length", "i"}, {"frames", "[]@probe_frame"}})},
	"initial_read":         {fields: []planField{{"mode", "i"}, {"frames", "[]$operation_frame"}, {"name", "s"}, {"resolved", "b"}, {"selector", "s"}}, optional: []string{"frames", "name", "resolved", "selector"}, masks: []int64{0, 15}},
	"example_start":        {fields: []planField{{"raw_0", "x"}, {"raw_1", "x"}}},
	"example_stop":         {fields: []planField{{"return_control", "x"}}},
	"examples":             {fields: []planField{{"qualification", "s"}, {"start", "[]@example_start"}, {"stop", "[]@example_stop"}}},
	"data_binding":         {fields: planFields("i", "control_enable_bit_1based data_byte_length data_byte_offset did encoding_mode mode5_bit_end_match mode5_bit_start_match record")},
	"choice":               {fields: []planField{{"text", "s"}, {"value", "i"}}},
	"presentation_pattern": {fields: planFields("i", "auto_continue_time button_size key key_invalid_flag key_operation_pattern lock_time maintenance_time pattern_display_key")},
	"scaling":              {fields: planJoin(planFields("i", "decimal_point_count div key mul offset unit_genre_id unit_key"), []planField{{"signed", "b"}, {"unit", "~s"}})},
	"signal":               {fields: []planField{{"choices", "[]@choice"}, {"engineering_to_raw", "$operation_conversion"}, {"pattern", "@presentation_pattern"}, {"physical", "@scaling"}}},
	"support_gate":         {fields: planJoin(planFields("s", "family inventory kind mode"), []planField{{"identifier", "i"}, {"length_probe", "s"}}), optional: []string{"length_probe"}, masks: []int64{0, 1}},
	"direct": {fields: planJoin(planFields("s", "execution kind name"), planFields("i", "id bit_end bit_start did encoding_mode positive_response runtime_length_minimum service"), planFields("s", "monitor_name session_requirement"), []planField{
		{"monitor_key", "~i"}, {"control_enable_mask", "$operation_mask"}, {"encoding_requirements", "$operation_encoding"}, {"data_id_for_act_records", "[]@data_binding"}, {"initial_read", "@initial_read"}, {"minimum_examples", "~@examples"}, {"runtime_length_probe", "@length_probe"}, {"signal_info", "@signal"}, {"start_frames", "[]@formula_frame"}, {"stop_frames", "[]@formula_frame"}, {"support_gate", "@support_gate"}, {"multi_control_group", "b"}, {"reason", "s"}}), optional: []string{"support_gate", "multi_control_group", "reason"}, masks: []int64{0, 1, 6}},
	"routine": {fields: planJoin(planFields("s", "execution kind name"), planFields("i", "id output_mask_button_variable output_mask_value_variable positive_response routine_command_variable routine_id routine_status_key routine_stop_command_variable service"), []planField{
		{"fixed_request", "b"}, {"session_requirement", "s"}, {"output_mask_button", "@variable"}, {"output_mask_value", "@variable"}, {"routine_commands", "[]@variable"}, {"routine_stop_commands", "[]@variable"}, {"start_frames", "[]@static_frame"}, {"stop_frames", "[]@static_frame"}, {"result_frames", "[]@static_frame"}, {"support_gate", "@support_gate"}}), optional: []string{"support_gate"}, masks: []int64{0, 1}},
	"refused": {fields: planJoin(planFields("s", "execution kind name"), planFields("i", "id record routine_id"), []planField{{"error", "s"}}), optional: []string{"routine_id"}, masks: []int64{0, 1}},
}
var operationPlanExternal = map[string]string{
	"$operation_base_frame": "operation_base_frames", "$operation_frame": "operation_frames",
	"$operation_conversion": "operation_conversions", "$operation_mask": "operation_mask_policies",
	"$operation_encoding": "operation_encoding_policies",
}

func (d *DB) operationPlan(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid operation plan reference")
	}
	if v, ok := d.operationPlans.load(id); ok {
		return v, nil
	}
	var roots [3]sql.NullInt64
	if err := d.db.QueryRowContext(ctx, "SELECT direct_id,routine_id,refused_id FROM operation_plan_entries WHERE id=?", id).Scan(&roots[0], &roots[1], &roots[2]); err != nil {
		return nil, fmt.Errorf("operation plan %d: %w", id, err)
	}
	names := []string{"direct", "routine", "refused"}
	name := ""
	var child int64
	for i, r := range roots {
		if r.Valid {
			if name != "" || r.Int64 <= 0 {
				return nil, fmt.Errorf("invalid operation plan root")
			}
			name = names[i]
			child = r.Int64
		}
	}
	if name == "" {
		return nil, fmt.Errorf("missing operation plan root")
	}
	out, err := d.planObject(ctx, name, child, 0)
	if err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	d.operationPlans.store(id, out, len(raw))
	return out, nil
}
func (d *DB) planObject(ctx context.Context, name string, id int64, depth int) (map[string]any, error) {
	schema, ok := operationPlanSchemas[name]
	if !ok || id <= 0 || depth > 24 {
		return nil, fmt.Errorf("invalid operation plan definition")
	}
	cols := []string{"present"}
	for _, f := range schema.fields {
		cols = append(cols, "f_"+f.name)
	}
	values := make([]any, len(cols))
	dest := make([]any, len(cols))
	for i := range values {
		dest[i] = &values[i]
	}
	if err := d.db.QueryRowContext(ctx, "SELECT "+strings.Join(cols, ",")+" FROM plan_"+name+" WHERE id=?", id).Scan(dest...); err != nil {
		return nil, fmt.Errorf("plan %s/%d: %w", name, id, err)
	}
	mask, ok := values[0].(int64)
	valid := ok && mask == 0
	for _, m := range schema.masks {
		if ok && m == mask {
			valid = true
		}
	}
	if !valid {
		return nil, fmt.Errorf("invalid plan presence mask")
	}
	optional := map[string]int{}
	for i, n := range schema.optional {
		optional[n] = i
	}
	out := map[string]any{}
	for i, f := range schema.fields {
		value := values[i+1]
		if bit, isOptional := optional[f.name]; isOptional && mask&(1<<bit) == 0 {
			if value != nil {
				return nil, fmt.Errorf("unexpected absent plan field %s", f.name)
			}
			continue
		}
		if strings.HasPrefix(f.kind, "[]") {
			count, ok := value.(int64)
			if !ok || count < 0 {
				return nil, fmt.Errorf("invalid plan child count")
			}
			rows, err := d.db.QueryContext(ctx, "SELECT position,value FROM plan_"+name+"_"+f.name+" WHERE parent_id=? ORDER BY position", id)
			if err != nil {
				return nil, err
			}
			children := []any{}
			var rowErr error
			for rows.Next() {
				var position int64
				var child any
				if err := rows.Scan(&position, &child); err != nil {
					rowErr = err
					break
				}
				if position != int64(len(children)) || position >= count {
					rowErr = fmt.Errorf("invalid plan child positions")
					break
				}
				children = append(children, child)
			}
			if rowErr == nil {
				rowErr = rows.Err()
			}
			rows.Close()
			if rowErr != nil {
				return nil, rowErr
			}
			if int64(len(children)) != count {
				return nil, fmt.Errorf("missing plan children")
			}
			decoded := make([]any, len(children))
			for j, c := range children {
				decoded[j], err = d.planValue(ctx, f.kind[2:], c, depth+1)
				if err != nil {
					return nil, err
				}
			}
			out[f.name] = decoded
		} else {
			v, err := d.planValue(ctx, f.kind, value, depth+1)
			if err != nil {
				return nil, fmt.Errorf("plan %s.%s: %w", name, f.name, err)
			}
			out[f.name] = v
		}
	}
	return out, nil
}
func (d *DB) planValue(ctx context.Context, kind string, value any, depth int) (any, error) {
	if strings.HasPrefix(kind, "~") {
		if value == nil {
			return nil, nil
		}
		kind = kind[1:]
	}
	if value == nil {
		return nil, fmt.Errorf("missing required value")
	}
	if kind == "x" {
		b, ok := value.([]byte)
		if !ok {
			return nil, fmt.Errorf("invalid command bytes")
		}
		return hex.EncodeToString(b), nil
	}
	n, ok := value.(int64)
	if !ok {
		return nil, fmt.Errorf("invalid plan integer")
	}
	if strings.HasPrefix(kind, "@") {
		return d.planObject(ctx, kind[1:], n, depth)
	}
	if strings.HasPrefix(kind, "$") {
		table, ok := operationPlanExternal[kind]
		if !ok || n <= 0 {
			return nil, fmt.Errorf("invalid plan external reference")
		}
		var found int
		if err := d.db.QueryRowContext(ctx, "SELECT 1 FROM "+table+" WHERE id=?", n).Scan(&found); err != nil {
			return nil, err
		}
		return map[string]any{kind: catalogNumber(n)}, nil
	}
	switch kind {
	case "i":
		return catalogNumber(n), nil
	case "b":
		if n != 0 && n != 1 {
			return nil, fmt.Errorf("invalid plan boolean")
		}
		return n == 1, nil
	case "s":
		var s string
		if n <= 0 {
			return nil, fmt.Errorf("invalid plan text reference")
		}
		if err := d.db.QueryRowContext(ctx, "SELECT value FROM text_values WHERE id=?", n).Scan(&s); err != nil {
			return nil, err
		}
		return s, nil
	}
	return nil, fmt.Errorf("unknown plan field type")
}
