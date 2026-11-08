package catalog

import (
	"context"
	"fmt"
)

// ListKind returns a bounded index listing. Category zero spans the region.
func (d *DB) ListKind(ctx context.Context, region string, category int, kind string, limit int) ([]Item, error) {
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 5000 {
		return nil, fmt.Errorf("limit must be between 1 and 5000")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT i.id,i.kind,i.region,i.category_id,coalesce(e.key,''),coalesce(e.name,''),i.identifier,i.name,i.detail
 FROM items i LEFT JOIN ecus e ON e.region=i.region AND e.category_id=i.category_id
 WHERE i.region=? AND (?=0 OR i.category_id=?) AND i.kind=?
 ORDER BY i.category_id,i.identifier COLLATE NOCASE,i.id LIMIT ?`, region, category, category, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Item{}
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Kind, &it.Region, &it.CategoryID, &it.ECU, &it.ECUName, &it.Identifier, &it.Name, &it.Detail); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}
