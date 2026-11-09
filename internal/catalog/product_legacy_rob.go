package catalog

import (
	"context"
	"database/sql"
	"fmt"
)

var legacyROBIntegerFields = []string{"bdid", "bit_end", "bit_start", "exception_handler_flag", "exception_handler_id", "graph_max", "graph_min", "name_string_index", "pattern_display_key", "physical_data_key", "presentation_mode", "signal_key", "sort_key", "support_mode"}
var legacyROBPhysicalFields = []string{"decimal_point_count", "div", "mul", "offset", "physical_data_key", "record", "sign_mode", "table", "unit_key"}

func (d *DB) legacyROBSignal(ctx context.Context, id int64) (map[string]any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid legacy ROB reference")
	}
	// Parent/entity queries complete before relationship reads: the catalog uses
	// one SQLite connection and must never issue nested reads with open rows.
	var definition, table, record int64
	var source string
	if err := d.db.QueryRowContext(ctx, "SELECT e.definition_id,t.value,e.table_number,e.record_number FROM legacy_rob_entries e JOIN text_values t ON t.id=e.source_id WHERE e.id=?", id).Scan(&definition, &source, &table, &record); err != nil {
		return nil, err
	}
	numbers := make([]int64, len(legacyROBIntegerFields))
	texts := make([]sql.NullString, 4)
	var low, high float64
	var patterns, count int64
	args := []any{}
	for i := range numbers {
		args = append(args, &numbers[i])
	}
	for i := range texts {
		args = append(args, &texts[i])
	}
	args = append(args, &low, &high, &patterns, &count)
	err := d.db.QueryRowContext(ctx, `SELECT d.bdid,d.bit_end,d.bit_start,d.exception_handler_flag,d.exception_handler_id,d.graph_max,d.graph_min,d.name_string_index,d.pattern_display_key,d.physical_data_key,d.presentation_mode,d.signal_key,d.sort_key,d.support_mode,n.value,s.value,c.value,r.value,d.data_min,d.data_max,d.pattern_list_id,d.physical_count
 FROM legacy_rob_definitions d LEFT JOIN text_values n ON n.id=d.name_id LEFT JOIN text_values s ON s.id=d.short_name_id LEFT JOIN text_values c ON c.id=d.decoder_id LEFT JOIN text_values r ON r.id=d.support_rule_id WHERE d.id=?`, definition).Scan(args...)
	if err != nil {
		return nil, err
	}
	out := map[string]any{"source": source, "table": signalNumber(table), "record": signalNumber(record), "data_min": signalNumber(low), "data_max": signalNumber(high)}
	for i, k := range legacyROBIntegerFields {
		out[k] = signalNumber(numbers[i])
	}
	for i, k := range []string{"name", "short_name", "decoder", "support_rule"} {
		out[k] = nil
		if texts[i].Valid {
			out[k] = texts[i].String
		}
	}
	variants := []any{}
	rows, err := d.db.QueryContext(ctx, `SELECT m.position,p.decimal_point_count,p.div,p.mul,p.offset,p.physical_data_key,p.record,p.sign_mode,p.table_number,p.unit_key,u.genre_id,t.value,u.string_index,u.record,u.table_type
 FROM legacy_rob_variants m JOIN legacy_rob_physical p ON p.id=m.physical_id JOIN freeze_units u ON u.id=p.unit_id LEFT JOIN text_values t ON t.id=u.text_id
 WHERE m.definition_id=? ORDER BY m.position`, definition)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pos, genre, index, unitRecord, unitTable int64
		values := make([]int64, len(legacyROBPhysicalFields))
		var name sql.NullString
		scan := []any{&pos}
		for i := range values {
			scan = append(scan, &values[i])
		}
		scan = append(scan, &genre, &name, &index, &unitRecord, &unitTable)
		if err = rows.Scan(scan...); err != nil {
			rows.Close()
			return nil, err
		}
		if pos != int64(len(variants)) || unitTable != 15 {
			rows.Close()
			return nil, fmt.Errorf("invalid legacy ROB physical relation")
		}
		v := map[string]any{}
		for i, k := range legacyROBPhysicalFields {
			v[k] = signalNumber(values[i])
		}
		var label any
		if name.Valid {
			label = name.String
		}
		v["units"] = []any{map[string]any{"genre_id": signalNumber(genre), "name": label, "name_string_index": signalNumber(index), "record": signalNumber(unitRecord)}}
		variants = append(variants, v)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if int64(len(variants)) != count {
		return nil, fmt.Errorf("incomplete legacy ROB physical relations")
	}
	out["physical_variants"] = variants
	var expected int64
	if err = d.db.QueryRowContext(ctx, "SELECT entry_count FROM freeze_pattern_lists WHERE id=?", patterns).Scan(&expected); err != nil {
		return nil, err
	}
	choices := []any{}
	rows, err = d.db.QueryContext(ctx, `SELECT m.position,t.value,p.string_index,p.record,p.table_type,p.value FROM freeze_pattern_members m JOIN freeze_patterns p ON p.id=m.pattern_id LEFT JOIN text_values t ON t.id=p.text_id WHERE m.list_id=? ORDER BY m.position`, patterns)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var pos, index, record, table, value int64
		var name sql.NullString
		if err = rows.Scan(&pos, &name, &index, &record, &table, &value); err != nil {
			rows.Close()
			return nil, err
		}
		if pos != int64(len(choices)) {
			rows.Close()
			return nil, fmt.Errorf("invalid legacy ROB pattern order")
		}
		var label any
		if name.Valid {
			label = name.String
		}
		choices = append(choices, map[string]any{"name": label, "name_string_index": signalNumber(index), "record": signalNumber(record), "table": signalNumber(table), "value": signalNumber(value)})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if int64(len(choices)) != expected {
		return nil, fmt.Errorf("incomplete legacy ROB pattern list")
	}
	out["pattern_display"] = choices
	return out, nil
}
