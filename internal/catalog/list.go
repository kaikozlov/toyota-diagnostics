package catalog

import (
	"context"
	"fmt"
)

const kindOwnerPredicate = ` AND i.category_id IN (
 SELECT c.category_id FROM item_count_rows c
 JOIN search_regions rg ON rg.id=c.region_id
 JOIN search_kinds kg ON kg.id=c.kind_id
 WHERE rg.name=? AND kg.name=?)`

// ListKind returns a bounded index listing. Category zero spans the region.
func (d *DB) ListKind(ctx context.Context, region string, category int, kind string, limit int) ([]Item, error) {
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	if limit < 1 || limit > 5000 {
		return nil, fmt.Errorf("limit must be between 1 and 5000")
	}
	query := `SELECT i.id,i.kind,i.region,i.category_id,coalesce(e.key,''),coalesce(e.name,''),i.identifier,i.name,i.detail
 FROM owner_items i LEFT JOIN ecus e ON e.region=i.region AND e.category_id=i.category_id
 WHERE i.region=? AND i.kind=?`
	args := []any{region, kind}
	if category != 0 {
		query += " AND i.category_id=?"
		args = append(args, category)
	} else {
		query += kindOwnerPredicate
		args = append(args, region, kind)
	}
	query += " ORDER BY i.category_id,i.identifier COLLATE NOCASE,i.id LIMIT ?"
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, query, args...)
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
