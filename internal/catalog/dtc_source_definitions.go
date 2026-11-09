package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
)

const sourceDTCQuery = `SELECT x.enabled_value,x.description_string_index,x.comment_string_index,
x.exception_handler_id,x.exception_handler_flag,x.detail_table,x.detail_lookup_key,
x.failure_string_index,x.info_byte,x.info_word,x.unknown_bytes,
s.value,e.table_number,e.record_number,c.value,d.value,m.value,f.value,
v.identifier,v.packed_code,v.field_mask,e.origin_mask,
v.description_id,v.comment_id,v.failure_id,e.source_id
FROM dtc_source_extensions x JOIN dtc_entries e ON e.id=x.entry_id
JOIN diagnostic_codes v ON v.id=e.code_id JOIN text_values c ON c.id=v.code_id
LEFT JOIN text_values s ON s.id=e.source_id LEFT JOIN text_values d ON d.id=v.description_id
LEFT JOIN text_values m ON m.id=v.comment_id LEFT JOIN text_values f ON f.id=v.failure_id WHERE x.id=?`

func (d *DB) dtcSourceDefinition(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid source DTC reference")
	}
	d.workStatements.mu.Lock()
	const key = "dtc_source_definition"
	stmt := d.workStatements.statements[key]
	if stmt == nil {
		var err error
		stmt, err = d.db.PrepareContext(ctx, sourceDTCQuery)
		if err != nil {
			d.workStatements.mu.Unlock()
			return nil, err
		}
		if d.workStatements.statements == nil {
			d.workStatements.statements = map[string]*sql.Stmt{}
		}
		d.workStatements.statements[key] = stmt
	}
	d.workStatements.mu.Unlock()
	values := make([]any, 26)
	dest := make([]any, len(values))
	for i := range dest {
		dest[i] = &values[i]
	}
	if err := stmt.QueryRowContext(ctx, id).Scan(dest...); err != nil {
		return nil, fmt.Errorf("source DTC %d: %w", id, err)
	}
	return decodeDTCSourceValues(values)
}
func decodeDTCSourceValues(v []any) (map[string]any, error) {
	fail := func() (map[string]any, error) { return nil, fmt.Errorf("invalid source DTC definition") }
	number := func(i int, bits uint) (int64, bool) {
		n, ok := v[i].(int64)
		return n, ok && n >= 0 && (bits == 63 || uint64(n) < uint64(1)<<bits)
	}
	widths := map[int]uint{0: 8, 1: 32, 2: 32, 3: 16, 4: 8, 12: 16, 13: 63, 20: 8, 21: 8}
	for i, bits := range widths {
		if _, ok := number(i, bits); !ok {
			return fail()
		}
	}
	table := v[12].(int64)
	if table != 5 && table != 65 {
		return fail()
	}
	if v[21].(int64) != 7 {
		return fail()
	}
	for _, pair := range [][2]int{{15, 22}, {16, 23}, {17, 24}, {11, 25}} {
		if (v[pair[0]] == nil) != (v[pair[1]] == nil) {
			return fail()
		}
	}
	for _, i := range []int{11, 14} {
		if _, ok := v[i].(string); !ok {
			return fail()
		}
	}
	for _, i := range []int{15, 16, 17} {
		if v[i] != nil {
			if _, ok := v[i].(string); !ok {
				return fail()
			}
		}
	}
	blob, ok := v[10].([]byte)
	if !ok {
		return fail()
	}
	n := func(x int64) json.Number { return json.Number(strconv.FormatInt(x, 10)) }
	val := func(i int) json.Number { return n(v[i].(int64)) }
	out := map[string]any{"source": v[11], "table": val(12), "record": val(13), "code": v[14],
		"description": v[15], "comment": v[16], "enabled_value": val(0), "enabled": v[0].(int64) != 0,
		"description_string_index": val(1), "comment_string_index": val(2),
		"exception_handler_id": val(3), "exception_handler_flag": val(4)}
	if table == 5 {
		if len(blob) != 2 || v[20].(int64) != 23 || v[7] != nil || v[8] != nil || v[9] != nil || v[17] != nil || v[19] != nil {
			return fail()
		}
		for _, i := range []int{5, 6, 18} {
			if _, ok := number(i, 16); !ok {
				return fail()
			}
		}
		out["dtc_identifier"] = val(18)
		out["detail_table"] = val(5)
		out["detail_lookup_key"] = val(6)
		out["unconsumed_descriptor_offsets"] = []any{n(24), n(25)}
		out["uninterpreted_bytes"] = map[string]any{"24_25": hex.EncodeToString(blob)}
	} else {
		if len(blob) != 1 || (v[20].(int64) != 43 && v[20].(int64) != 47) || v[5] != nil || v[6] != nil || v[18] != nil {
			return fail()
		}
		for i, bits := range map[int]uint{7: 32, 8: 8, 9: 16, 19: 32} {
			if _, ok := number(i, bits); !ok {
				return fail()
			}
		}
		packed := v[19].(int64)
		out["packed_dtc"] = n(packed)
		out["base_dtc"] = n((packed >> 8) & 65535)
		out["failure_type"] = n(packed & 255)
		out["failure_description"] = v[17]
		out["failure_string_index"] = val(7)
		out["diag_code_info_fields"] = []any{map[string]any{"source_offset": n(65), "output_offset": n(52), "width": n(1), "value": val(8)}, map[string]any{"source_offset": n(62), "output_offset": n(54), "width": n(2), "value": val(9)}}
		out["uninterpreted_bytes"] = map[string]any{"67": hex.EncodeToString(blob)}
	}
	return out, nil
}
