package catalog

import (
	"cmp"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strconv"
	"strings"
)

// search runs the ranked full-text query; category 0 means every ECU and an
// empty kind means every kind.
//
// Ranking is a single policy, not stacked special cases:
//
//	0 navigational ECU (kind=ecu and key, category id, or full ECU name)
//	1 exact identifier in the query's number domain (explicit 0x stays
//	  hex-only; a bare token may match either 0x160 or decimal 160)
//	2 prefix of a hex (0x…) identifier
//	3 exact item name
//	4 other records belonging to an ECU whose key equals the query
//	5 BM25 over name (10), detail (1), identifier (3)
//
// Ties stay stable: kind, category, identifier, then ID.
//
// BM25 has to score every match in scope, so the statement that does it reads
// as little else as it can: the full-text index skips IDs outside the scope's
// owner ranges, only the few records that can reach tiers 1–3 have their text
// compared, and SQLite keeps only the best rows. Text is read for those.
func (d *DB) search(ctx context.Context, region string, category int, kind, query string, limit int) ([]Item, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	if limit > 5000 {
		limit = 5000
	}
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	match := ftsQuery(query)
	if match == "" {
		return []Item{}, nil
	}
	scope, err := d.scope(ctx, region, category)
	if err != nil || scope.empty {
		return []Item{}, err
	}
	first, last := scope.first, scope.last
	candidates, hexExact, decExact, hexPrefix := exactForms(query)
	var categoryText any // cast(category_id AS text)=query, as an integer
	if n, err := strconv.Atoi(query); err == nil && strconv.Itoa(n) == query {
		categoryText = n
	}
	args := []any{region, query, kind, candidates, hexExact, decExact, hexPrefix, match, first, last, category, categoryText}
	// ?1 region, ?2 query, ?3 kind, ?4 the full-text query for tiers 1–3,
	// ?5–?7 the identifier forms, ?8 match, ?9–?10 the scope's bounds,
	// ?11 category, ?12 the query as a category ID, ?13 limit. lower(),
	// LIKE and NOCASE all fold ASCII only, as in every tier.
	keyECU := `CASE WHEN o.category_id IN (SELECT category_id FROM ecus WHERE region=?1 AND key=?2 COLLATE NOCASE) THEN 4 ELSE 5 END`
	rank := `SELECT f.rowid, CASE
  WHEN o.category_id IN (
    SELECT category_id FROM ecus WHERE region=?1 AND key=?2 COLLATE NOCASE UNION ALL
    SELECT category_id FROM ecus WHERE region=?1 AND name=?2 COLLATE NOCASE UNION ALL
    SELECT category_id FROM ecus WHERE region=?1 AND category_id=?12)
   AND (SELECT kind_id FROM search_records WHERE id=o.record_id)=(SELECT id FROM search_kinds WHERE name='ecu') THEN 0
  WHEN f.rowid IN (SELECT rowid FROM items_fts WHERE items_fts MATCH ?4 AND rowid BETWEEN ?9 AND ?10) THEN coalesce((SELECT CASE
    WHEN ?5!='' AND lower(x.value)=?5 THEN 1
    WHEN ?6!='' AND lower(x.value)=?6 THEN 1
    WHEN lower(x.value)=lower(?2) THEN 1
    WHEN ?7!='' AND lower(x.value) LIKE ?7 THEN 2
    WHEN lower(n.value)=lower(?2) THEN 3 END
   FROM search_records r JOIN text_values x ON x.id=r.identifier_id JOIN text_values n ON n.id=r.name_id
   WHERE r.id=o.record_id), ` + keyECU + `)
  ELSE ` + keyECU + ` END AS tier, bm25(items_fts, 10.0, 1.0, 3.0) AS score
FROM items_fts f CROSS JOIN item_occurrences o ON o.id=f.rowid
WHERE items_fts MATCH ?8 AND f.rowid BETWEEN ?9 AND ?10
  AND o.region_id=(SELECT id FROM search_regions WHERE name=?1) AND (?11=0 OR o.category_id=?11)
  AND (?3='' OR (SELECT kind_id FROM search_records WHERE id=o.record_id)=(SELECT id FROM search_kinds WHERE name=?3))
ORDER BY tier, score LIMIT ?13`

	type hit struct {
		id    int64
		tier  int
		score float64
	}
	same := func(a, b hit) bool { return a.tier == b.tier && a.score == b.score }
	var hits []hit
	stmt, err := d.prepared(ctx, rank)
	if err != nil {
		return nil, err
	}
	// SQLite cuts a run of equal scores arbitrarily, so fetch past the limit
	// and widen until the limit-th row's tie is complete.
	for n := 4*limit + 256; ; n *= 4 {
		hits = hits[:0]
		rows, err := stmt.QueryContext(ctx, append(args, n)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var h hit
			if err := rows.Scan(&h.id, &h.tier, &h.score); err != nil {
				rows.Close()
				return nil, err
			}
			hits = append(hits, h)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if len(hits) < n || !same(hits[n-1], hits[limit-1]) {
			break
		}
	}
	if len(hits) > limit {
		end := limit
		for end < len(hits) && same(hits[end], hits[limit-1]) {
			end++
		}
		hits = hits[:end]
	}
	if len(hits) == 0 {
		return []Item{}, nil
	}
	ids := make([]int64, len(hits))
	for i, h := range hits {
		ids[i] = h.id
	}
	items, err := d.itemsByID(ctx, region, ids)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(hits, func(a, b hit) int {
		ia, ib := items[a.id], items[b.id]
		switch {
		case a.tier != b.tier:
			return a.tier - b.tier
		case a.score != b.score:
			return cmp.Compare(a.score, b.score)
		case ia.Kind != ib.Kind:
			return strings.Compare(ia.Kind, ib.Kind)
		case ia.CategoryID != ib.CategoryID:
			return ia.CategoryID - ib.CategoryID
		case ia.Identifier != ib.Identifier:
			return strings.Compare(ia.Identifier, ib.Identifier)
		}
		return cmp.Compare(a.id, b.id)
	})
	out := make([]Item, min(limit, len(hits)))
	for i := range out {
		out[i] = items[hits[i].id]
	}
	return out, nil
}

// span is a run of occurrence IDs; empty holds none.
type span struct {
	first, last int64
	empty       bool
}

// scope returns the IDs a region or one of its ECUs can hold. Verify checks
// that every occurrence lies inside an owner range of its own region and ECU
// (ECU listings rely on the same fact), so the full-text index can skip IDs
// outside them; IDs inside are still checked per row. The catalog is
// immutable, so each scope is read once.
func (d *DB) scope(ctx context.Context, region string, category int) (span, error) {
	type key struct {
		region   string
		category int
	}
	if s, ok := d.scopes.Load(key{region, category}); ok {
		return s.(span), nil
	}
	lo, hi := category, category
	if category == 0 {
		lo, hi = math.MinInt64, math.MaxInt64
	}
	var first, last sql.NullInt64
	err := d.db.QueryRowContext(ctx, `SELECT min(first_id), max(last_id) FROM owner_ranges
WHERE region_id=(SELECT id FROM search_regions WHERE name=?) AND category_id BETWEEN ? AND ?`, region, lo, hi).Scan(&first, &last)
	if err != nil {
		return span{}, err
	}
	s := span{first.Int64, last.Int64, !last.Valid}
	d.scopes.Store(key{region, category}, s)
	return s, nil
}

// verifyOwnerRanges checks that every occurrence lies inside an owner range
// of its own region and ECU. Once no two ranges of an ECU overlap, the
// occurrences counted inside each range are each counted once.
func verifyOwnerRanges(ctx context.Context, db *sql.DB) error {
	var overlaps, outside int64
	if err := db.QueryRowContext(ctx, `SELECT
 (SELECT count(*) FROM (SELECT first_id, max(last_id) OVER (PARTITION BY region_id, category_id ORDER BY first_id
   ROWS BETWEEN UNBOUNDED PRECEDING AND 1 PRECEDING) AS before FROM owner_ranges) WHERE before>=first_id),
 (SELECT count(*) FROM item_occurrences) - coalesce((SELECT sum((SELECT count(*) FROM item_occurrences o
   WHERE o.id BETWEEN w.first_id AND w.last_id AND o.region_id=w.region_id AND o.category_id=w.category_id)) FROM owner_ranges w), 0)`).Scan(&overlaps, &outside); err != nil {
		return err
	}
	if overlaps != 0 {
		return fmt.Errorf("catalog: %d owner ranges overlap another range of their ECU", overlaps)
	}
	if outside != 0 {
		return fmt.Errorf("catalog: %d occurrences lie outside their ECU's owner ranges", outside)
	}
	return nil
}

// exactForms describes tiers 1–3. They compare a record's own identifier or
// name with the query, so only records whose identifier or name starts with
// the query's tokens can qualify; candidates finds those in the full-text
// index. An explicit 0x stays hex-only; a bare token may also be decimal.
func exactForms(query string) (candidates, hexExact, decExact, hexPrefix string) {
	bare, explicit := queryIdentForm(query)
	phrase := `^"` + strings.ReplaceAll(query, `"`, `""`) + `"`
	identifiers := phrase
	if bare != "" {
		hexExact = "0x" + bare
		hexPrefix = hexExact + "%"
		identifiers += ` OR ^"` + hexExact + `"*`
		if !explicit {
			decExact = bare
			identifiers += ` OR ^"` + bare + `"`
		}
	}
	return "{identifier} : (" + identifiers + ") OR {name} : " + phrase, hexExact, decExact, hexPrefix
}

// itemsByID reads the region's items by occurrence ID.
func (d *DB) itemsByID(ctx context.Context, region string, ids []int64) (map[int64]Item, error) {
	list, _ := json.Marshal(ids)
	stmt, err := d.prepared(ctx, `SELECT o.id, k.name, o.category_id, coalesce(e.key,''), coalesce(e.name,''), x.value, n.value, t.value
FROM json_each(?2) j JOIN item_occurrences o ON o.id=j.value JOIN search_records r ON r.id=o.record_id
JOIN search_kinds k ON k.id=r.kind_id JOIN search_text x ON x.id=r.identifier_id
JOIN search_text n ON n.id=r.name_id JOIN search_text t ON t.id=r.detail_id
LEFT JOIN ecus e ON e.region=?1 AND e.category_id=o.category_id`)
	if err != nil {
		return nil, err
	}
	rows, err := stmt.QueryContext(ctx, region, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := make(map[int64]Item, len(ids))
	for rows.Next() {
		item := Item{Region: region}
		if err := rows.Scan(&item.ID, &item.Kind, &item.CategoryID, &item.ECU, &item.ECUName, &item.Identifier, &item.Name, &item.Detail); err != nil {
			return nil, err
		}
		items[item.ID] = item
	}
	return items, rows.Err()
}
