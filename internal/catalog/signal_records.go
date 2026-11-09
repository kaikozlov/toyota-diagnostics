package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"
)

func (d *DB) signalInfo(ctx context.Context, id int64) (any, error) {
	r := &signalReader{db: d, ctx: ctx, active: map[int64]bool{}}
	return r.info(id)
}
func (d *DB) didSupport(ctx context.Context, id int64) (any, error) {
	r := &signalReader{db: d, ctx: ctx, active: map[int64]bool{}}
	return r.support(id)
}
func (d *DB) supportCondition(ctx context.Context, id int64) (any, error) {
	r := &signalReader{db: d, ctx: ctx, active: map[int64]bool{}}
	return r.condition(id)
}

// Signal definitions and display rows are projections of shared typed monitor
// semantics. They are runtime data, not lazy provenance.
type signalReader struct {
	db     *DB
	ctx    context.Context
	active map[int64]bool
}

func (r *signalReader) row(table string, id int64, n int) ([]any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid %s reference %d", table, id)
	}
	values := make([]any, n+1)
	dest := make([]any, n+1)
	for i := range values {
		dest[i] = &values[i]
	}
	if err := r.db.db.QueryRowContext(r.ctx, "SELECT * FROM "+table+" WHERE id=?", id).Scan(dest...); err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("missing %s record %d", table, id)
		}
		return nil, fmt.Errorf("%s record %d: %w", table, id, err)
	}
	values = values[1:]
	kinds := map[string]string{
		"text_values": "s", "signal_labels": "iiii", "signal_pattern_sets": "i",
		"signal_infos": "iiiiiiiIiii", "signal_did_support": "iiiiiiii",
		"signal_conditions": "iiiiiiiiibii", "signal_monitors": "iiiIIiiiiiiiffiiiiii",
		"signal_definitions": "iIIIiIIIIIIIIIII", "data_list_rows": "iIIIiI",
	}[table]
	if len(kinds) != len(values) {
		return nil, fmt.Errorf("invalid typed signal schema for %s", table)
	}
	for i, kind := range kinds {
		v := values[i]
		if kind == 'I' && v == nil {
			continue
		}
		valid := false
		switch kind {
		case 'i', 'I':
			_, valid = v.(int64)
		case 's':
			_, valid = v.(string)
		case 'f':
			_, valid = v.(float64)
		case 'b':
			_, valid = v.([]byte)
		}
		if !valid {
			return nil, fmt.Errorf("invalid %s column %d", table, i)
		}
	}
	var required []int
	if table == "signal_definitions" {
		if values[0] == int64(5) {
			required = []int{1, 2, 3, 5, 6, 7, 9, 10, 11, 12, 13, 14}
		}
		if values[0] == int64(6) {
			required = []int{15}
		}
	}
	if table == "data_list_rows" {
		if values[0] == int64(5) {
			required = []int{1, 2}
		}
		if values[0] == int64(6) {
			required = []int{3, 5}
		}
	}
	for _, i := range required {
		if values[i] == nil {
			return nil, fmt.Errorf("missing %s column %d", table, i)
		}
	}
	return values, nil
}

func signalID(v any) (int64, error) {
	n, ok := v.(int64)
	if !ok || n <= 0 {
		return 0, fmt.Errorf("invalid signal entity reference %v", v)
	}
	return n, nil
}
func signalNumber(v any) any {
	switch x := v.(type) {
	case int64:
		return json.Number(strconv.FormatInt(x, 10))
	case float64:
		format := byte('g')
		// Canonical source JSON uses Python float formatting: fixed notation
		// from 1e-4 through 1e16, retaining .0 for integral real values.
		if x == 0 || (math.Abs(x) >= 1e-4 && math.Abs(x) < 1e16) {
			format = 'f'
		}
		s := strconv.FormatFloat(x, format, -1, 64)
		if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return json.Number(s)
	default:
		return v
	}
}
func signalBool(v any) (bool, error) {
	n, ok := v.(int64)
	if !ok || (n != 0 && n != 1) {
		return false, fmt.Errorf("invalid signal boolean %v", v)
	}
	return n == 1, nil
}
func (r *signalReader) text(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	id, err := signalID(v)
	if err != nil {
		return nil, err
	}
	row, err := r.row("text_values", id, 1)
	if err != nil {
		return nil, err
	}
	value, ok := row[0].(string)
	if !ok {
		return nil, fmt.Errorf("invalid signal text %d", id)
	}
	return value, nil
}
func (r *signalReader) cached(tag int64, id int64, fn func() (any, error)) (any, error) {
	key := int64(0)
	if id > 0 && id < (1<<55) {
		key = (tag << 55) | id
		if v, ok := r.db.signalRecords.load(key); ok {
			return v, nil
		}
	}
	value, err := fn()
	if err != nil {
		return nil, err
	}
	if key != 0 {
		packed, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		r.db.signalRecords.store(key, value, len(packed))
	}
	return value, nil
}
func (r *signalReader) label(id int64) (map[string]any, error) {
	value, err := r.cached(1, id, func() (any, error) {
		v, err := r.row("signal_labels", id, 4)
		if err != nil {
			return nil, err
		}
		name, err := r.text(v[1])
		if err != nil {
			return nil, err
		}
		return map[string]any{"monitor_key": signalNumber(v[0]), "name": name, "bit_start": signalNumber(v[2]), "bit_end": signalNumber(v[3])}, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}
func (r *signalReader) patterns(id int64) (any, error) {
	return r.cached(2, id, func() (any, error) {
		set, err := r.row("signal_pattern_sets", id, 1)
		if err != nil {
			return nil, err
		}
		rows, err := r.db.db.QueryContext(r.ctx, "SELECT k.value,p.value_id,v.value FROM signal_pattern_entries p LEFT JOIN text_values k ON k.id=p.key_id LEFT JOIN text_values v ON v.id=p.value_id WHERE p.set_id=? ORDER BY k.value", id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		out := map[string]any{}
		for rows.Next() {
			var key string
			var valueID sql.NullInt64
			var value sql.NullString
			if err := rows.Scan(&key, &valueID, &value); err != nil {
				return nil, err
			}
			if valueID.Valid && !value.Valid {
				return nil, fmt.Errorf("missing signal pattern text")
			}
			if value.Valid {
				out[key] = value.String
			} else {
				out[key] = nil
			}
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if int64(len(out)) != set[0].(int64) {
			return nil, fmt.Errorf("missing signal pattern entries")
		}
		return out, nil
	})
}
func (r *signalReader) info(id int64) (map[string]any, error) {
	value, err := r.cached(3, id, func() (any, error) {
		v, err := r.row("signal_infos", id, 11)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for i, k := range []string{"bit_width", "decimal_point_count", "div", "mul", "offset", "physical_data_key", "sign_mode"} {
			out[k] = signalNumber(v[i])
		}
		legacy, err := signalBool(v[10])
		if err != nil {
			return nil, err
		}
		if legacy {
			signed, err := signalBool(v[6])
			if err != nil {
				return nil, err
			}
			delete(out, "sign_mode")
			out["signed"] = signed
		}
		out["unit"], err = r.text(v[7])
		if err != nil {
			return nil, err
		}
		out["unit_key"] = signalNumber(v[8])
		p, err := signalID(v[9])
		if err != nil {
			return nil, err
		}
		out["pattern_display"], err = r.patterns(p)
		return out, err
	})
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}
func (r *signalReader) support(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	id, err := signalID(v)
	if err != nil {
		return nil, err
	}
	if r.active[id] {
		return nil, fmt.Errorf("cyclic DID support %d", id)
	}
	r.active[id] = true
	defer delete(r.active, id)
	return r.cached(4, id, func() (any, error) {
		row, err := r.row("signal_did_support", id, 8)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for i, k := range []string{"did", "fixed_length", "record", "snapshot_length_encoding", "support_mode", "table"} {
			out[k] = signalNumber(row[i])
		}
		out["snapshot_length_encoding"], err = r.text(row[3])
		if err != nil {
			return nil, err
		}
		has, err := signalBool(row[6])
		if err != nil {
			return nil, err
		}
		rows, err := r.db.db.QueryContext(r.ctx, "SELECT position,child_id FROM signal_support_additional WHERE parent_id=? ORDER BY position", id)
		if err != nil {
			return nil, err
		}
		ids := []int64{}
		for rows.Next() {
			var position, child int64
			if err := rows.Scan(&position, &child); err != nil {
				rows.Close()
				return nil, err
			}
			if position != int64(len(ids)) {
				rows.Close()
				return nil, fmt.Errorf("noncontiguous DID support children")
			}
			ids = append(ids, child)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(ids)) != row[7].(int64) {
			return nil, fmt.Errorf("missing additional DID support")
		}
		if !has && len(ids) != 0 {
			return nil, fmt.Errorf("unexpected additional DID support")
		}
		if has {
			children := make([]any, 0, len(ids))
			for _, child := range ids {
				value, err := r.support(child)
				if err != nil {
					return nil, err
				}
				children = append(children, value)
			}
			out["additional_records"] = children
		}
		return out, nil
	})
}
func (r *signalReader) condition(v any) (any, error) {
	if v == nil {
		return nil, nil
	}
	id, err := signalID(v)
	if err != nil {
		return nil, err
	}
	return r.cached(5, id, func() (any, error) {
		row, err := r.row("signal_conditions", id, 12)
		if err != nil {
			return nil, err
		}
		out := map[string]any{}
		for i, k := range []string{"bit_end", "bit_start", "comparison_mode", "comparison_value", "condition_type", "key", "record", "referenced_did", "table"} {
			out[k] = signalNumber(row[i])
		}
		raw, ok := row[9].([]byte)
		if !ok {
			return nil, fmt.Errorf("invalid signal variable bytes")
		}
		out["variable_bytes"] = hex.EncodeToString(raw)
		out["variable_id"] = signalNumber(row[10])
		view := row[11].(int64)
		if view < 0 || view > 2 {
			return nil, fmt.Errorf("invalid support-condition representation")
		}
		if view != 0 {
			delete(out, "comparison_mode")
			delete(out, "comparison_value")
		}
		if view == 2 {
			delete(out, "record")
			delete(out, "table")
			delete(out, "variable_bytes")
		}
		return out, nil
	})
}
func copySignalMap(in map[string]any) map[string]any {
	out := make(map[string]any, len(in)+20)
	for k, v := range in {
		out[k] = v
	}
	return out
}
func (r *signalReader) monitor(id int64) (map[string]any, error) {
	value, err := r.cached(6, id, func() (any, error) {
		v, err := r.row("signal_monitors", id, 20)
		if err != nil {
			return nil, err
		}
		label, err := signalID(v[0])
		if err != nil {
			return nil, err
		}
		base, err := r.label(label)
		if err != nil {
			return nil, err
		}
		out := copySignalMap(base)
		out["alternate_did"] = signalNumber(v[1])
		info, err := signalID(v[2])
		if err != nil {
			return nil, err
		}
		out["signal_info"], err = r.info(info)
		if err != nil {
			return nil, err
		}
		out["did_support"], err = r.support(v[3])
		if err != nil {
			return nil, err
		}
		out["support_condition"], err = r.condition(v[4])
		if err != nil {
			return nil, err
		}
		for i, k := range map[int]string{5: "extraction_mode", 6: "flags", 10: "pattern_display_key", 11: "physical_data_key", 14: "primary_did", 15: "record", 16: "sort_key", 18: "table", 19: "visibility_override"} {
			out[k] = signalNumber(v[i])
		}
		out["graph_range"] = []any{signalNumber(v[7]), signalNumber(v[8])}
		out["physical_range"] = []any{signalNumber(v[12]), signalNumber(v[13])}
		out["list_member"], err = signalBool(v[9])
		if err != nil {
			return nil, err
		}
		out["source"], err = r.text(v[17])
		return out, err
	})
	if err != nil {
		return nil, err
	}
	return value.(map[string]any), nil
}
func (d *DB) signalDefinition(ctx context.Context, id int64) (any, error) {
	r := &signalReader{db: d, ctx: ctx, active: map[int64]bool{}}
	return r.cached(7, id, func() (any, error) {
		v, err := r.row("signal_definitions", id, 16)
		if err != nil {
			return nil, err
		}
		generation, ok := v[0].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid signal generation")
		}
		decoder, err := r.text(v[4])
		if err != nil {
			return nil, err
		}
		if generation == 5 {
			label, err := signalID(v[1])
			if err != nil {
				return nil, err
			}
			base, err := r.label(label)
			if err != nil {
				return nil, err
			}
			out := copySignalMap(base)
			for i, k := range map[int]string{2: "alternate_did", 3: "decimal_point_count", 5: "div", 6: "mul", 7: "offset"} {
				out[k] = signalNumber(v[i])
			}
			out["decoder"] = decoder
			out["unit"], err = r.text(v[8])
			if err != nil {
				return nil, err
			}
			out["graph_range"] = []any{signalNumber(v[9]), signalNumber(v[10])}
			out["data_range"] = []any{signalNumber(v[11]), signalNumber(v[12])}
			pattern, err := signalID(v[13])
			if err != nil {
				return nil, err
			}
			out["patterns"], err = r.patterns(pattern)
			if err != nil {
				return nil, err
			}
			out["signed"], err = signalBool(v[14])
			return out, err
		}
		if generation == 6 {
			monitor, err := signalID(v[15])
			if err != nil {
				return nil, err
			}
			base, err := r.monitor(monitor)
			if err != nil {
				return nil, err
			}
			out := copySignalMap(base)
			info := out["signal_info"].(map[string]any)
			for _, k := range []string{"bit_width", "decimal_point_count", "div", "mul", "offset", "pattern_display", "sign_mode", "unit", "unit_key"} {
				value, ok := info[k]
				if !ok {
					return nil, fmt.Errorf("missing P6 signal info field %s", k)
				}
				out[k] = value
			}
			out["decoder"] = decoder
			return out, nil
		}
		return nil, fmt.Errorf("unsupported signal generation %d", generation)
	})
}
func (d *DB) dataListRow(ctx context.Context, id int64) (any, error) {
	r := &signalReader{db: d, ctx: ctx, active: map[int64]bool{}}
	return r.cached(8, id, func() (any, error) {
		v, err := r.row("data_list_rows", id, 6)
		if err != nil {
			return nil, err
		}
		generation, ok := v[0].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid data-list generation")
		}
		did, err := r.text(v[4])
		if err != nil {
			return nil, err
		}
		if generation == 5 {
			label, err := signalID(v[1])
			if err != nil {
				return nil, err
			}
			base, err := r.label(label)
			if err != nil {
				return nil, err
			}
			out := copySignalMap(base)
			out["sort_key"] = signalNumber(v[2])
			out["did"] = did
			return out, nil
		}
		if generation == 6 {
			monitor, err := signalID(v[3])
			if err != nil {
				return nil, err
			}
			base, err := r.monitor(monitor)
			if err != nil {
				return nil, err
			}
			out := copySignalMap(base)
			out["did"] = did
			out["composite_records"] = []any{}
			out["wire_field"], err = signalBool(v[5])
			return out, err
		}
		return nil, fmt.Errorf("unsupported data-list generation %d", generation)
	})
}
