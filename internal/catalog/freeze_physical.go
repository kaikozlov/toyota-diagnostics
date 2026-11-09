package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
)

func (d *DB) freezePhysical(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid freeze physical reference")
	}
	fields := []string{"bit_width", "decimal_point_count", "div", "mul", "offset", "physical_data_key", "record", "sign_mode", "table", "unit_key"}
	var values [12]int64
	err := d.db.QueryRowContext(ctx, `SELECT bit_width,decimal_point_count,div,mul,offset,physical_data_key,record,sign_mode,table_type,unit_key,unit_id,pattern_list_id FROM freeze_physical_defs WHERE id=?`, id).Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &values[7], &values[8], &values[9], &values[10], &values[11])
	if err != nil {
		return nil, fmt.Errorf("freeze physical definition %d: %w", id, err)
	}
	n := func(v int64) json.Number { return json.Number(strconv.FormatInt(v, 10)) }
	out := map[string]any{}
	for i, key := range fields {
		out[key] = n(values[i])
	}
	var genre, ordinal, index, table int64
	var label sql.NullString
	err = d.db.QueryRowContext(ctx, `SELECT u.genre_id,u.record,u.string_index,u.table_type,t.value FROM freeze_units u LEFT JOIN text_values t ON t.id=u.text_id WHERE u.id=?`, values[10]).Scan(&genre, &ordinal, &index, &table, &label)
	if err != nil {
		return nil, fmt.Errorf("physical unit: %w", err)
	}
	nullable := func(v sql.NullString) any {
		if v.Valid {
			return v.String
		}
		return nil
	}
	out["units"] = []any{map[string]any{"genre_id": n(genre), "record": n(ordinal), "string_index": n(index), "table": n(table), "text": nullable(label)}}
	var count int
	if err = d.db.QueryRowContext(ctx, "SELECT entry_count FROM freeze_pattern_lists WHERE id=?", values[11]).Scan(&count); err != nil {
		return nil, fmt.Errorf("physical pattern list: %w", err)
	}
	if count < 0 {
		return nil, fmt.Errorf("invalid physical pattern count")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT m.position,p.record,p.string_index,p.table_type,t.value,p.value FROM freeze_pattern_members m JOIN freeze_patterns p ON p.id=m.pattern_id LEFT JOIN text_values t ON t.id=p.text_id WHERE m.list_id=? ORDER BY m.position`, values[11])
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	patterns := []any{}
	for rows.Next() {
		var position int
		var value int64
		if err = rows.Scan(&position, &ordinal, &index, &table, &label, &value); err != nil {
			return nil, err
		}
		if position != len(patterns) {
			return nil, fmt.Errorf("invalid physical pattern order")
		}
		patterns = append(patterns, map[string]any{"record": n(ordinal), "string_index": n(index), "table": n(table), "text": nullable(label), "value": n(value)})
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	if len(patterns) != count {
		return nil, fmt.Errorf("incomplete physical pattern list")
	}
	out["patterns"] = patterns
	return out, nil
}
