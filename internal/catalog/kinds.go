package catalog

import "context"

// Kinds returns the actual record families, including kinds added by future exporters.
// Empty region and zero category select the whole catalog.
func (d *DB) Kinds(ctx context.Context, region string, category int) ([]string, error) {
	rows, err := d.db.QueryContext(ctx, "SELECT DISTINCT kind FROM items WHERE (?='' OR region=?) AND (?=0 OR category_id=?) ORDER BY kind", region, region, category, category)
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
