package catalog

import (
	"context"
	"encoding/hex"
	"fmt"
)

func (d *DB) freezeUnknownFields(ctx context.Context, id int64) (map[string]any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid freeze unknown-fields reference")
	}
	var a, b, c, e any
	var last int64
	err := d.db.QueryRowContext(ctx, "SELECT bytes_34_3f,bytes_48_4b,bytes_56_57,bytes_5e_5f,byte_67 FROM legacy_freeze_unknown_facts WHERE id=?", id).Scan(&a, &b, &c, &e, &last)
	if err != nil {
		return nil, fmt.Errorf("freeze unknown fields %d: %w", id, err)
	}
	out := map[string]any{}
	for _, field := range []struct {
		name  string
		value any
		size  int
	}{{"34_3f", a, 12}, {"48_4b", b, 4}, {"56_57", c, 2}, {"5e_5f", e, 2}} {
		data, ok := field.value.([]byte)
		if !ok || len(data) != field.size {
			return nil, fmt.Errorf("invalid freeze unknown bytes %s", field.name)
		}
		out[field.name] = hex.EncodeToString(data)
	}
	if last < 0 || last > 255 {
		return nil, fmt.Errorf("invalid freeze unknown byte")
	}
	out["67"] = catalogNumber(last)
	return out, nil
}
