package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func legacyConditionValue(values [7]int64, variable []byte, resolved bool, source string, table, record, hasKind int64) (map[string]any, error) {
	if (table != 8 && table != 9) || record < 0 || (hasKind != 0 && hasKind != 1) {
		return nil, fmt.Errorf("invalid semantic legacy condition origin")
	}
	key, variableID, start, end, mode, exceptionID, exceptionFlag := values[0], values[1], values[2], values[3], values[4], values[5], values[6]
	var query any
	var offsets []int64
	length := 0
	switch mode {
	case 2, 3, 4:
		selector, response := int64(16), int64(2)
		length = 1
		offsets = []int64{1}
		if mode == 3 {
			selector = 105
		}
		if mode == 4 {
			selector = 106
			response = 4
			length = 2
			offsets = []int64{2, 3}
		}
		positions := []any{}
		for _, v := range offsets {
			positions = append(positions, signalNumber(v))
		}
		query = map[string]any{"request_selector": signalNumber(selector), "variable_length": signalNumber(int64(length)), "request_offsets": positions, "response_offset": signalNumber(response)}
	case 6:
		query = map[string]any{"cache": "did_support", "variable_min_length": signalNumber(int64(1)), "response_offset": signalNumber(int64(0))}
	}
	status := "invalid_geometry_or_mode"
	if query != nil && end >= start {
		status = "valid"
	}
	var gate, gateP4 any
	if mode == 2 {
		gate = "pid"
		gateP4 = "pid"
	} else if mode == 3 {
		gate = "did"
	} else if mode == 6 {
		gateP4 = "did"
	}
	out := map[string]any{"key": signalNumber(key), "variable_id": signalNumber(variableID), "bit_start": signalNumber(start), "bit_end": signalNumber(end), "source_mode": signalNumber(mode),
		"exception_id": signalNumber(exceptionID), "exception_flag": signalNumber(exceptionFlag), "source": source, "table": signalNumber(table), "record": signalNumber(record),
		"predicate": "nonzero", "bit_order": "msb0", "query": query, "native_status": status,
		"supported_identifier_gate": map[string]any{"CheckSupportBit": gate, "CheckSupportBitOldBody": nil, "CheckSupportBitP4": gateP4}}
	if hasKind == 1 {
		kind := "pid_bit"
		if table == 9 {
			kind = "did_bit"
		}
		out["kind"] = kind
	}
	if mode == 2 || mode == 6 {
		cache := "pid_support"
		if mode == 6 {
			cache = "did_support"
		}
		out["p4_cache_query"] = map[string]any{"cache": cache, "identifier_variable_offset": signalNumber(int64(0)), "response_offset": signalNumber(int64(0)), "predicate": "nonzero"}
	}
	if resolved {
		out["variable"] = hex.EncodeToString(variable)
		if query != nil {
			if mode == 6 {
				if len(variable) == 0 {
					out["native_status"] = "invalid_variable_length"
				} else {
					out["cache_identifier"] = signalNumber(int64(variable[0]))
				}
			} else if len(variable) != length {
				out["native_status"] = "invalid_variable_length"
			} else {
				patches := []any{}
				for i, b := range variable {
					patches = append(patches, map[string]any{"offset": signalNumber(offsets[i]), "value": signalNumber(int64(b))})
				}
				out["request_patches"] = patches
			}
		}
	}
	return out, nil
}
func legacyResponseValue(values [4]int64, source string, table, record int64) (map[string]any, error) {
	if (table != 6 && table != 7) || record < 0 {
		return nil, fmt.Errorf("invalid semantic legacy response origin")
	}
	kind := "pid"
	if table == 7 {
		kind = "did"
	}
	resolution := "fixed"
	if values[1] == 0 {
		resolution = "runtime"
	}
	return map[string]any{"key": signalNumber(values[0]), "data_length": signalNumber(values[1]), "exception_id": signalNumber(values[2]), "exception_flag": signalNumber(values[3]),
		"source": source, "table": signalNumber(table), "record": signalNumber(record), "kind": kind, "length_resolution": resolution}, nil
}
func legacyDynamicValue(values [6]int64, record int64) (map[string]any, error) {
	if record < 0 {
		return nil, fmt.Errorf("invalid semantic legacy dynamic origin")
	}
	out := map[string]any{"table": signalNumber(int64(39)), "record": signalNumber(record)}
	for i, k := range []string{"monitor_key", "mul", "div", "bit_start", "bit_end", "pid"} {
		out[k] = signalNumber(values[i])
	}
	return out, nil
}
func (d *DB) legacyCondition(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid legacy condition reference")
	}
	if value, ok := d.legacyConditions.load(id); ok {
		return value, nil
	}
	var values [7]int64
	var variable []byte
	var resolved bool
	var source string
	var table, record, kind int64
	err := d.db.QueryRowContext(ctx, `SELECT d.condition_key,d.variable_id,d.bit_start,d.bit_end,d.source_mode,d.exception_id,d.exception_flag,d.variable,d.variable IS NOT NULL,
 s.value,e.table_number,e.record_number,e.has_kind FROM legacy_condition_entries e
 JOIN legacy_condition_defs d ON d.id=e.definition_id JOIN text_values s ON s.id=e.source_id WHERE e.id=?`, id).
		Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &values[6], &variable, &resolved, &source, &table, &record, &kind)
	if err != nil {
		return nil, fmt.Errorf("legacy condition %d: %w", id, err)
	}
	value, err := legacyConditionValue(values, variable, resolved, source, table, record, kind)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d.legacyConditions.store(id, value, len(encoded))
	return value, nil
}
func (d *DB) legacyResponse(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid legacy response reference")
	}
	if value, ok := d.legacyResponses.load(id); ok {
		return value, nil
	}
	var values [4]int64
	var source string
	var table, record int64
	err := d.db.QueryRowContext(ctx, `SELECT d.response_key,d.data_length,d.exception_id,d.exception_flag,s.value,e.table_number,e.record_number
 FROM legacy_response_entries e JOIN legacy_response_defs d ON d.id=e.definition_id JOIN text_values s ON s.id=e.source_id WHERE e.id=?`, id).
		Scan(&values[0], &values[1], &values[2], &values[3], &source, &table, &record)
	if err != nil {
		return nil, fmt.Errorf("legacy response %d: %w", id, err)
	}
	value, err := legacyResponseValue(values, source, table, record)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d.legacyResponses.store(id, value, len(encoded))
	return value, nil
}
func (d *DB) legacyDynamic(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid legacy dynamic reference")
	}
	if value, ok := d.legacyDynamics.load(id); ok {
		return value, nil
	}
	var values [6]int64
	var record int64
	var unknownID sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT d.monitor_key,d.mul,d.div,d.bit_start,d.bit_end,d.pid,e.record_number,e.unknown_id
 FROM legacy_dynamic_entries e JOIN legacy_dynamic_defs d ON d.id=e.definition_id WHERE e.id=?`, id).
		Scan(&values[0], &values[1], &values[2], &values[3], &values[4], &values[5], &record, &unknownID)
	if err != nil {
		return nil, fmt.Errorf("legacy dynamic %d: %w", id, err)
	}
	value, err := legacyDynamicValue(values, record)
	if err != nil {
		return nil, err
	}
	if unknownID.Valid {
		unknown, err := d.legacyUnknownFields(ctx, "dynamic", unknownID.Int64)
		if err != nil {
			return nil, err
		}
		value["uninterpreted_bytes"] = unknown
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d.legacyDynamics.store(id, value, len(encoded))
	return value, nil
}
