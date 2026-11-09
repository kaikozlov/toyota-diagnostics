package catalog

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
)

// Membership caches retain at most one normalized collection per domain.
// They never contain expanded diagnostic definitions.
type membershipCache struct {
	mu    sync.Mutex
	id    int64
	value any
}
type membershipGroup struct {
	key   string
	count int
	rows  []any
}

func (d *DB) membership(ctx context.Context, kind string, id int64) (any, error) {
	var prefix, column, target, marker string
	var cache *membershipCache
	switch kind {
	case "$did_membership":
		prefix = "did"
		column = "definition_id"
		target = "signal_definitions"
		marker = "$signal_definition"
		cache = &d.didMembership
	case "$dtc_membership":
		prefix = "dtc"
		column = "entry_id"
		target = "dtc_entries"
		marker = "$diagnostic_code"
		cache = &d.dtcMembership
	case "$display_membership":
		prefix = "display"
		column = "row_id"
		target = "data_list_rows"
		marker = "$data_list_row"
		cache = &d.displayMembership
	default:
		return nil, fmt.Errorf("unknown membership %q", kind)
	}
	if id <= 0 {
		return nil, fmt.Errorf("invalid membership ID")
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.id == id && cache.value != nil {
		return cache.value, nil
	}
	var groups []membershipGroup
	if prefix == "display" {
		var count int
		if err := d.db.QueryRowContext(ctx, "SELECT row_count FROM display_membership_sets WHERE id=?", id).Scan(&count); err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, fmt.Errorf("negative display membership count")
		}
		groups = []membershipGroup{{count: count, rows: []any{}}}
	} else {
		var count int
		if err := d.db.QueryRowContext(ctx, "SELECT group_count FROM "+prefix+"_membership_sets WHERE id=?", id).Scan(&count); err != nil {
			return nil, err
		}
		if count < 0 {
			return nil, fmt.Errorf("negative membership group count")
		}
		rows, err := d.db.QueryContext(ctx, "SELECT position,key,member_count FROM "+prefix+"_membership_groups WHERE set_id=? ORDER BY position", id)
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for rows.Next() {
			var position, n int
			var key string
			if err = rows.Scan(&position, &key, &n); err != nil {
				rows.Close()
				return nil, err
			}
			if position != len(groups) || n < 0 || seen[key] {
				rows.Close()
				return nil, fmt.Errorf("invalid membership group sequence")
			}
			seen[key] = true
			groups = append(groups, membershipGroup{key: key, count: n, rows: []any{}})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if len(groups) != count {
			return nil, fmt.Errorf("missing membership group")
		}
	}
	groupExpr := "r.group_position"
	order := "r.group_position,r.position"
	if prefix == "display" {
		groupExpr = "0"
		order = "r.position"
	}
	query := "SELECT " + groupExpr + ",r.position,r." + column + ",t.id FROM " + prefix + "_membership_rows r LEFT JOIN " + target + " t ON t.id=r." + column + " WHERE r.set_id=? ORDER BY " + order
	rows, err := d.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var group, position int
		var ref int64
		var exists sql.NullInt64
		if err = rows.Scan(&group, &position, &ref, &exists); err != nil {
			rows.Close()
			return nil, err
		}
		if group < 0 || group >= len(groups) || position != len(groups[group].rows) || ref <= 0 || !exists.Valid {
			rows.Close()
			return nil, fmt.Errorf("invalid or missing membership definition")
		}
		groups[group].rows = append(groups[group].rows, map[string]any{marker: catalogNumber(ref)})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		if len(g.rows) != g.count {
			return nil, fmt.Errorf("missing membership row")
		}
	}
	var value any
	if prefix == "display" {
		value = groups[0].rows
	} else {
		out := make(map[string]any, len(groups))
		for _, g := range groups {
			out[g.key] = g.rows
		}
		value = out
	}
	// Conservative estimate includes maximum int64 reference width and marker
	// syntax, plus group keys. Large sets are decoded but never retained.
	size := 0
	for _, g := range groups {
		if len(g.rows) > (1<<20)/(len(marker)+32) {
			size = (1 << 20) + 1
			break
		}
		size += len(g.rows)*(len(marker)+32) + len(g.key) + 16
		if size > 1<<20 {
			break
		}
	}
	if size <= 1<<20 {
		cache.id, cache.value = id, value
	}
	return value, nil
}
