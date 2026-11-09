package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
)

// diagnosticCode reads decoded diagnostic facts. It does not interpret firmware
// bytes.
func (d *DB) diagnosticCode(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid diagnostic code reference %d", id)
	}
	if value, ok := d.diagnosticCodes.load(id); ok {
		return value, nil
	}
	var code string
	var description, comment, failure, source sql.NullString
	var identifier, packed, table, record sql.NullInt64
	var mask, originMask int64
	var descriptionID, commentID, failureID, sourceID sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT c.value,d.value,m.value,f.value,v.identifier,v.packed_code,v.field_mask,
 s.value,e.table_number,e.record_number,e.origin_mask,v.description_id,v.comment_id,v.failure_id,e.source_id
 FROM dtc_entries e JOIN diagnostic_codes v ON v.id=e.code_id
 JOIN text_values c ON c.id=v.code_id LEFT JOIN text_values d ON d.id=v.description_id
 LEFT JOIN text_values m ON m.id=v.comment_id LEFT JOIN text_values f ON f.id=v.failure_id
 LEFT JOIN text_values s ON s.id=e.source_id WHERE e.id=?`, id).Scan(&code, &description, &comment, &failure, &identifier, &packed, &mask, &source, &table, &record, &originMask, &descriptionID, &commentID, &failureID, &sourceID)
	if err != nil {
		return nil, fmt.Errorf("diagnostic code %d: %w", id, err)
	}
	if mask < 0 || mask & ^int64(63) != 0 || originMask < 0 || originMask & ^int64(7) != 0 {
		return nil, fmt.Errorf("invalid diagnostic code field mask")
	}
	for _, p := range []struct {
		text sql.NullString
		id   sql.NullInt64
	}{{description, descriptionID}, {comment, commentID}, {failure, failureID}, {source, sourceID}} {
		if p.text.Valid != p.id.Valid {
			return nil, fmt.Errorf("missing diagnostic code text")
		}
	}
	out := map[string]any{"code": code}
	if mask&1 != 0 {
		out["kind"] = "dtc"
	}
	for _, field := range []struct {
		name string
		bit  int64
		text sql.NullString
	}{{"description", 2, description}, {"comment", 4, comment}, {"failure", 8, failure}} {
		if mask&field.bit != 0 {
			var value any
			if field.text.Valid {
				value = field.text.String
			}
			out[field.name] = value
		} else if field.text.Valid {
			return nil, fmt.Errorf("unexpected diagnostic code %s", field.name)
		}
	}
	if identifier.Valid && identifier.Int64 < 0 {
		return nil, fmt.Errorf("invalid diagnostic identifier")
	}
	if packed.Valid && (packed.Int64 < 0 || packed.Int64 > 0xffffffff) {
		return nil, fmt.Errorf("invalid packed diagnostic code")
	}
	if mask&16 != 0 {
		var value any
		if identifier.Valid {
			value = json.Number(strconv.FormatInt(identifier.Int64, 10))
		}
		out["dtc_identifier"] = value
	} else if identifier.Valid {
		return nil, fmt.Errorf("unexpected diagnostic identifier")
	}
	if mask&32 != 0 {
		var value any
		if packed.Valid {
			value = fmt.Sprintf("0x%06X", packed.Int64)
		}
		out["packed_dtc"] = value
	} else if packed.Valid {
		return nil, fmt.Errorf("unexpected packed diagnostic code")
	}
	if originMask&1 != 0 {
		var value any
		if source.Valid {
			value = source.String
		}
		out["source"] = value
	} else if source.Valid {
		return nil, fmt.Errorf("unexpected diagnostic code source")
	}
	for _, field := range []struct {
		name   string
		bit    int64
		number sql.NullInt64
	}{{"table", 2, table}, {"record", 4, record}} {
		if field.number.Valid && field.number.Int64 < 0 {
			return nil, fmt.Errorf("invalid diagnostic code %s", field.name)
		}
		if originMask&field.bit != 0 {
			var value any
			if field.number.Valid {
				value = json.Number(strconv.FormatInt(field.number.Int64, 10))
			}
			out[field.name] = value
		} else if field.number.Valid {
			return nil, fmt.Errorf("unexpected diagnostic code %s", field.name)
		}
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	d.diagnosticCodes.store(id, out, len(encoded))
	return out, nil
}
