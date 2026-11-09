package catalog

import (
	"context"
	"encoding/hex"
	"fmt"
	"strings"
)

var legacyUnknownKeys = map[string][]string{
	"operation": {"0_19", "60_61", "70_73", "80_81", "83", "86_87", "90_91"},
	"pattern":   {"14_17", "22_23"},
	"dynamic":   {"14_16", "18_end"},
}
var legacyUnknownLengths = map[string][]int{"operation": {20, 2, 4, 2, 1, 2, 2}, "pattern": {4, 2}, "dynamic": {3, -1}}

func (d *DB) legacyUnknownFields(ctx context.Context, domain string, id int64) (map[string]any, error) {
	keys, ok := legacyUnknownKeys[domain]
	if !ok || id <= 0 {
		return nil, fmt.Errorf("invalid legacy unknown reference")
	}
	cols := []string{}
	joins := []string{}
	for i, key := range keys {
		alias := fmt.Sprintf("t%d", i)
		cols = append(cols, alias+".value")
		joins = append(joins, "LEFT JOIN text_values "+alias+" ON "+alias+".id=f.range_"+key+"_id")
	}
	vals := make([]any, len(cols))
	dest := make([]any, len(cols))
	for i := range dest {
		dest[i] = &vals[i]
	}
	if err := d.db.QueryRowContext(ctx, "SELECT "+strings.Join(cols, ",")+" FROM legacy_"+domain+"_unknown_facts f "+strings.Join(joins, " ")+" WHERE f.id=?", id).Scan(dest...); err != nil {
		return nil, err
	}
	out := map[string]any{}
	for i, key := range keys {
		text, ok := vals[i].(string)
		if !ok {
			return nil, fmt.Errorf("missing legacy unknown range")
		}
		raw, err := hex.DecodeString(text)
		if err != nil || hex.EncodeToString(raw) != text || (legacyUnknownLengths[domain][i] >= 0 && len(raw) != legacyUnknownLengths[domain][i]) {
			return nil, fmt.Errorf("invalid legacy unknown range")
		}
		out[key] = text
	}
	return out, nil
}
