package catalog

import (
	"context"
	"strings"
)

// Kinds returns the actual record families, as stored.
// Empty region and zero category select the whole catalog.
func (d *DB) Kinds(ctx context.Context, region string, category int) ([]string, error) {
	query := `SELECT DISTINCT k.name FROM item_count_rows o
JOIN search_kinds k ON k.id=o.kind_id
JOIN search_regions g ON g.id=o.region_id`
	var filters []string
	var args []any
	if region != "" {
		filters = append(filters, "g.name=?")
		args = append(args, region)
	}
	if category != 0 {
		filters = append(filters, "o.category_id=?")
		args = append(args, category)
	}
	if len(filters) > 0 {
		query += " WHERE " + strings.Join(filters, " AND ")
	}
	query += " ORDER BY 1"
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var kind string
		if err := rows.Scan(&kind); err != nil {
			return nil, err
		}
		out = append(out, kind)
	}
	return out, rows.Err()
}
