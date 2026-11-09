package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
)

func restoreROBUnknown(out map[string]any, value any, table int64) error {
	if value == nil {
		return nil
	}
	blob, ok := value.([]byte)
	if !ok {
		return fmt.Errorf("invalid ROB unknown storage")
	}
	unknown := map[string]any{}
	switch table {
	case 163:
		if len(blob) != 3 {
			return fmt.Errorf("invalid ROB163 unknown width")
		}
		unknown["65_67"] = hex.EncodeToString(blob)
	case 164:
		if len(blob) != 7 {
			return fmt.Errorf("invalid ROB164 unknown width")
		}
		unknown["38_39"] = hex.EncodeToString(blob[:2])
		unknown["43_47"] = hex.EncodeToString(blob[2:])
	default:
		return fmt.Errorf("unexpected ROB unknown table")
	}
	out["uninterpreted_bytes"] = unknown
	return nil
}

func (d *DB) behaviorCode(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid behavior code reference")
	}
	if v, ok := d.behaviorCodes.load(id); ok {
		return v, nil
	}
	var variant, table, record int64
	var numbers [8]sql.NullInt64
	var texts [5]sql.NullString
	var ids [5]sql.NullInt64
	var source string
	var unknown any
	dest := []any{&variant}
	for i := range numbers {
		dest = append(dest, &numbers[i])
	}
	for i := range texts {
		dest = append(dest, &texts[i], &ids[i])
	}
	dest = append(dest, &source, &table, &record, &unknown)
	err := d.db.QueryRowContext(ctx, `SELECT d.variant,d.behavior_code,d.comment_string_index,d.division_string_index,d.enabled,
 d.exception_handler_flag,d.exception_handler_id,d.memory_override,d.name_string_index,
 n.value,d.name_id,c.value,d.comment_id,s.value,d.signature_id,v.value,d.division_id,k.value,d.kind_id,
 origin.value,e.table_number,e.record_number,d.unknown_bytes FROM behavior_code_entries e JOIN behavior_code_defs d ON d.id=e.definition_id
 LEFT JOIN text_values n ON n.id=d.name_id LEFT JOIN text_values c ON c.id=d.comment_id
 LEFT JOIN text_values s ON s.id=d.signature_id LEFT JOIN text_values v ON v.id=d.division_id
 LEFT JOIN text_values k ON k.id=d.kind_id JOIN text_values origin ON origin.id=e.source_id WHERE e.id=?`, id).Scan(dest...)
	if err != nil {
		return nil, fmt.Errorf("behavior code %d: %w", id, err)
	}
	if variant != 0 && variant != 1 {
		return nil, fmt.Errorf("invalid behavior code variant")
	}
	out := map[string]any{"source": source, "table": catalogNumber(table), "record": catalogNumber(record)}
	names := []string{"behavior_code", "comment_string_index", "division_string_index", "enabled", "exception_handler_flag", "exception_handler_id", "memory_override", "name_string_index"}
	for i, name := range names {
		if variant == 0 && i > 0 {
			if numbers[i].Valid {
				return nil, fmt.Errorf("unexpected P5 behavior field")
			}
			continue
		}
		if !numbers[i].Valid {
			return nil, fmt.Errorf("missing behavior numeric field")
		}
		out[name] = catalogNumber(numbers[i].Int64)
	}
	for i, name := range []string{"name", "comment", "signature", "division", "kind"} {
		if ids[i].Valid != texts[i].Valid {
			return nil, fmt.Errorf("missing behavior text")
		}
		if variant == 0 && name == "division" || variant == 1 && name == "kind" {
			if ids[i].Valid {
				return nil, fmt.Errorf("unexpected behavior text")
			}
			continue
		}
		if !texts[i].Valid {
			if name == "signature" || name == "kind" {
				return nil, fmt.Errorf("missing required behavior text")
			}
			out[name] = nil
		} else {
			out[name] = texts[i].String
		}
	}
	if unknown != nil && variant != 1 {
		return nil, fmt.Errorf("unexpected P5 ROB unknown bytes")
	}
	if err := restoreROBUnknown(out, unknown, table); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	d.behaviorCodes.store(id, out, len(raw))
	return out, nil
}
