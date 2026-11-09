package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
)

var freezeSignalIntegers = []string{"begin_condition", "bit_end", "bit_start", "data_type", "exception_handler_flag", "exception_handler_id", "graph_max", "graph_min", "membership_flags", "monitor_key", "name_string_index", "pattern_display_key", "physical_data_key", "pid", "slope_condition", "slope_default", "sort_key", "support_condition_key", "support_policy", "trigger_condition"}
var freezeSignalBooleans = []string{"freeze_member", "live_member", "support_condition_resolved"}

func (d *DB) legacyFreezeSignal(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid legacy freeze reference")
	}
	if value, ok := d.legacyFreezeSignals.load(id); ok {
		return value, nil
	}
	rows, err := d.db.QueryContext(ctx, `SELECT d.*,n.value AS name_value,sn.value AS short_name_value,l.value AS layout_value,s.value AS source_value,
 e.table_number,e.record_number,e.unknown_id FROM legacy_freeze_entries e JOIN legacy_freeze_definitions d ON d.id=e.definition_id
 LEFT JOIN text_values n ON n.id=d.name_id JOIN text_values sn ON sn.id=d.short_name_id
 JOIN text_values l ON l.id=d.layout_id JOIN text_values s ON s.id=e.source_id WHERE e.id=?`, id)
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
		return nil, fmt.Errorf("missing legacy freeze signal")
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
	for i, key := range columns {
		data[key] = values[i]
	}
	definition, ok := data["id"].(int64)
	if !ok || definition <= 0 {
		return nil, fmt.Errorf("invalid freeze definition")
	}
	if (data["name_id"] == nil) != (data["name_value"] == nil) {
		return nil, fmt.Errorf("missing freeze name")
	}
	out := map[string]any{"kind": "freeze_frame", "name": data["name_value"]}
	for key, column := range map[string]string{"short_name": "short_name_value", "layout": "layout_value", "source": "source_value"} {
		text, ok := data[column].(string)
		if !ok {
			return nil, fmt.Errorf("missing freeze text %s", key)
		}
		out[key] = text
	}
	for key, column := range map[string]string{"table": "table_number", "record": "record_number"} {
		n, ok := data[column].(int64)
		if !ok || n < 0 {
			return nil, fmt.Errorf("invalid freeze origin")
		}
		out[key] = signalNumber(n)
	}
	for _, key := range freezeSignalIntegers {
		n, ok := data[key].(int64)
		if !ok {
			return nil, fmt.Errorf("missing freeze integer %s", key)
		}
		out[key] = signalNumber(n)
	}
	for _, key := range freezeSignalBooleans {
		n, ok := data[key].(int64)
		if !ok || (n != 0 && n != 1) {
			return nil, fmt.Errorf("invalid freeze flag %s", key)
		}
		out[key] = n == 1
	}
	for _, key := range []string{"data_min", "data_max"} {
		n, ok := data[key].(float64)
		if !ok || math.IsInf(n, 0) || math.IsNaN(n) {
			return nil, fmt.Errorf("invalid freeze range")
		}
		out[key] = signalNumber(n)
	}
	physical, ok := data["physical_id"].(int64)
	if !ok || physical <= 0 {
		return nil, fmt.Errorf("invalid freeze physical")
	}
	out["physical_records"] = []any{map[string]any{"$freeze_physical": signalNumber(physical)}}
	for _, relation := range []struct{ field, table, column, count, marker string }{
		{"support_conditions", "legacy_freeze_support_members", "condition_id", "support_count", "$legacy_condition"},
		{"dynamic_lsb_records", "legacy_freeze_dynamic_members", "dynamic_id", "dynamic_count", "$legacy_dynamic"},
	} {
		count, ok := data[relation.count].(int64)
		if !ok || count < 0 {
			return nil, fmt.Errorf("invalid freeze relationship count")
		}
		rows, err := d.db.QueryContext(ctx, "SELECT position,"+relation.column+" FROM "+relation.table+" WHERE definition_id=? ORDER BY position", definition)
		if err != nil {
			return nil, err
		}
		children := []any{}
		for rows.Next() {
			var pos, child int64
			if err = rows.Scan(&pos, &child); err != nil {
				rows.Close()
				return nil, err
			}
			if pos != int64(len(children)) || child <= 0 {
				rows.Close()
				return nil, fmt.Errorf("invalid freeze relationship")
			}
			children = append(children, map[string]any{relation.marker: signalNumber(child)})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(children)) != count {
			return nil, fmt.Errorf("incomplete freeze relationships")
		}
		out[relation.field] = children
	}
	if value := data["unknown_id"]; value != nil {
		unknownID, ok := value.(int64)
		if !ok || unknownID <= 0 {
			return nil, fmt.Errorf("invalid freeze unknown-fields reference")
		}
		unknown, err := d.freezeUnknownFields(ctx, unknownID)
		if err != nil {
			return nil, err
		}
		out["unassigned_fields"] = unknown
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	d.legacyFreezeSignals.store(id, out, len(encoded))
	return out, nil
}
