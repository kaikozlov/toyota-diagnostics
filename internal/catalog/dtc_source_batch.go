package catalog

import (
	"context"
	"fmt"
	"strings"
)

func (d *DB) dtcSourceArray(ctx context.Context, nodes []any, active referenceStack) ([]any, bool, error) {
	const marker = "$dtc_source_definition"
	if len(nodes) < 32 {
		return nil, false, nil
	}
	ids := make([]int64, len(nodes))
	for i, node := range nodes {
		ref, ok := node.(map[string]any)
		if !ok || len(ref) != 1 {
			return nil, false, nil
		}
		value, ok := ref[marker]
		if !ok {
			return nil, false, nil
		}
		id, e := referenceID(value, marker)
		if e != nil {
			return nil, true, e
		}
		if active[referenceKey{marker, id}] {
			return nil, true, fmt.Errorf("cyclic source DTC reference")
		}
		ids[i] = id
	}
	out := make([]any, len(ids))
	for start := 0; start < len(ids); start += 256 {
		end := start + 256
		if end > len(ids) {
			end = len(ids)
		}
		args := make([]any, end-start)
		for i := range args {
			args[i] = ids[start+i]
		}
		query := strings.Replace(sourceDTCQuery, "SELECT ", "SELECT x.id,", 1)
		query = strings.TrimSuffix(query, "WHERE x.id=?") + "WHERE x.id IN(" + strings.TrimSuffix(strings.Repeat("?,", len(args)), ",") + ")"
		rows, e := d.db.QueryContext(ctx, query, args...)
		if e != nil {
			return nil, true, e
		}
		found := map[int64]any{}
		for rows.Next() {
			v := make([]any, 27)
			dest := make([]any, 27)
			for i := range dest {
				dest[i] = &v[i]
			}
			if e = rows.Scan(dest...); e != nil {
				rows.Close()
				return nil, true, e
			}
			id, ok := v[0].(int64)
			if !ok {
				rows.Close()
				return nil, true, fmt.Errorf("invalid source DTC ID")
			}
			value, err := decodeDTCSourceValues(v[1:])
			if err != nil {
				rows.Close()
				return nil, true, err
			}
			found[id] = value
		}
		e = rows.Err()
		rows.Close()
		if e != nil {
			return nil, true, e
		}
		for i := start; i < end; i++ {
			value, ok := found[ids[i]]
			if !ok {
				return nil, true, fmt.Errorf("missing source DTC definition %d", ids[i])
			}
			out[i] = value
		}
	}
	return out, true, nil
}
