// Package catalog reads the indexed, lossless SQLite projection of GTS+.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"

	_ "modernc.org/sqlite"
)

const schema = "toyota-diagnostics-sqlite-v1"

type DB struct {
	db   *sql.DB
	Path string
}

type Item struct {
	Kind       string `json:"kind"`
	Region     string `json:"region"`
	CategoryID int    `json:"category_id"`
	ECU        string `json:"ecu"`
	ECUName    string `json:"ecu_name"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Detail     string `json:"detail"`
}

type ECU struct {
	Region        string         `json:"region"`
	CategoryID    int            `json:"category_id"`
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	Database      string         `json:"database"`
	Generation    *int           `json:"generation,omitempty"`
	SupportMode   string         `json:"support_mode"`
	CatalogMember string         `json:"catalog_member,omitempty"`
	Counts        map[string]int `json:"counts"`
}

type Vehicle struct {
	Region        string `json:"region"`
	Type          int    `json:"vehicle_type"`
	Name          string `json:"name"`
	InstallSetIDs []int  `json:"install_set_ids"`
}

// VehicleECU is a declared Toyota install-set row, not a live mount probe.
type VehicleECU struct {
	CategoryID       int    `json:"category_id"`
	Key              string `json:"key"`
	Name             string `json:"name"`
	InstallSetID     int    `json:"install_set_id"`
	RouteSelector    int    `json:"route_selector"`
	RequestAddress   *int   `json:"request_address,omitempty"`
	AddressExtension *int   `json:"address_extension,omitempty"`
	TransportKind    string `json:"transport_kind"`
}

type Stats struct {
	Schema        string `json:"schema"`
	SourceSchema  string `json:"source_schema"`
	Release       string `json:"release"`
	DefaultRegion string `json:"default_region"`
	Members       int    `json:"members"`
	Items         int    `json:"items"`
	Vehicles      int    `json:"vehicles"`
	ECUs          int    `json:"ecus"`
}

func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("no catalog configured; pass --db PATH or set TOYOTA_CATALOG")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	dsn := (&url.URL{Scheme: "file", Path: abs, RawQuery: "mode=ro&immutable=1"}).String()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	d := &DB{db: db, Path: abs}
	var version string
	if err := db.QueryRow("SELECT value FROM meta WHERE key='schema'").Scan(&version); err != nil {
		db.Close()
		return nil, fmt.Errorf("invalid Toyota catalog %s: %w", abs, err)
	}
	if version != schema {
		db.Close()
		return nil, fmt.Errorf("unsupported Toyota catalog schema %q (want %q)", version, schema)
	}
	return d, nil
}

func (d *DB) Close() error { return d.db.Close() }

func (d *DB) Meta(ctx context.Context, key string) (string, error) {
	var value string
	err := d.db.QueryRowContext(ctx, "SELECT value FROM meta WHERE key=?", key).Scan(&value)
	return value, err
}

func (d *DB) Stats(ctx context.Context) (Stats, error) {
	var s Stats
	for _, row := range []struct {
		key string
		dst *string
	}{
		{"schema", &s.Schema}, {"source_schema", &s.SourceSchema},
		{"release", &s.Release}, {"default_region", &s.DefaultRegion},
	} {
		value, err := d.Meta(ctx, row.key)
		if err != nil {
			return s, err
		}
		*row.dst = value
	}
	members, err := d.Meta(ctx, "member_count")
	if err != nil {
		return s, err
	}
	s.Members, err = strconv.Atoi(members)
	if err != nil {
		return s, fmt.Errorf("invalid member_count %q", members)
	}
	for _, row := range []struct {
		query string
		dst   *int
	}{
		{"SELECT count(*) FROM items", &s.Items},
		{"SELECT count(*) FROM vehicles", &s.Vehicles},
		{"SELECT count(*) FROM ecus", &s.ECUs},
	} {
		if err := d.db.QueryRowContext(ctx, row.query).Scan(row.dst); err != nil {
			return s, err
		}
	}
	return s, nil
}

func (d *DB) Verify(ctx context.Context) error {
	var result string
	if err := d.db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&result); err != nil {
		return err
	}
	if result != "ok" {
		return fmt.Errorf("SQLite quick_check: %s", result)
	}
	return nil
}

func Region(text string) (string, error) {
	r := strings.ToUpper(strings.TrimSpace(text))
	if r != "NA" && r != "EU" && r != "JP" {
		return "", fmt.Errorf("unknown region %q; choose NA, EU or JP", text)
	}
	return r, nil
}

// FTS5 query construction accepts only ordinary Unicode alphanumerics, so
// query punctuation cannot be interpreted as MATCH syntax.
func ftsQuery(query string) string {
	words := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	terms := make([]string, 0, len(words))
	for _, word := range words {
		// Prefix matching allows LTA, steer, and multiword queries as typed.
		if len([]rune(word)) <= 1 {
			continue
		}
		terms = append(terms, `"`+strings.ToLower(word)+`"*`)
	}
	return strings.Join(terms, " AND ")
}

func (d *DB) Search(ctx context.Context, region, query string, limit int) ([]Item, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive")
	}
	if limit > 1000 {
		limit = 1000
	}
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	match := ftsQuery(query)
	if match == "" {
		return nil, nil
	}
	// All fields in the source item remain addressable by kind, region and ECU.
	rows, err := d.db.QueryContext(ctx, `
SELECT i.kind, i.region, i.category_id, coalesce(e.key,''), coalesce(e.name,''),
       i.identifier, i.name, i.detail
FROM items_fts
JOIN items i ON i.id=items_fts.rowid
LEFT JOIN ecus e ON e.region=i.region AND e.category_id=i.category_id
WHERE items_fts MATCH ? AND i.region=?
ORDER BY CASE WHEN lower(e.key)=lower(?) AND i.kind='ecu' THEN -2
              WHEN lower(e.key)=lower(?) THEN -1
              WHEN lower(i.name)=lower(?) THEN 0
              WHEN lower(i.identifier)=lower(?) THEN 1 ELSE 2 END,
         bm25(items_fts, 10.0, 1.0, 3.0),
         i.kind, i.category_id, i.identifier, i.id
LIMIT ?`, match, region, query, query, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Item, 0)
	for rows.Next() {
		var r Item
		if err := rows.Scan(&r.Kind, &r.Region, &r.CategoryID, &r.ECU, &r.ECUName, &r.Identifier, &r.Name, &r.Detail); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanECU(rows *sql.Rows) (ECU, error) {
	var e ECU
	var generation sql.NullInt64
	var member sql.NullString
	var mode sql.NullString
	var counts string
	err := rows.Scan(&e.Region, &e.CategoryID, &e.Key, &e.Name, &e.Database, &generation, &mode, &member, &counts)
	if err != nil {
		return e, err
	}
	if generation.Valid {
		n := int(generation.Int64)
		e.Generation = &n
	}
	e.SupportMode = mode.String
	e.CatalogMember = member.String
	if err := json.Unmarshal([]byte(counts), &e.Counts); err != nil {
		return e, err
	}
	return e, nil
}

const selectECUs = `SELECT region,category_id,key,name,database_name,generation,support_mode,catalog_member,counts FROM ecus`

func (d *DB) ECUs(ctx context.Context, region, query string, limit int) ([]ECU, error) {
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := d.db.QueryContext(ctx, selectECUs+` WHERE region=? AND
  (key LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR cast(category_id as text)=?)
ORDER BY CASE WHEN lower(key)=lower(?) THEN 0 ELSE 1 END, category_id LIMIT ?`,
		region, pattern, pattern, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]ECU, 0)
	for rows.Next() {
		row, err := scanECU(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (d *DB) ECU(ctx context.Context, region, query string) (ECU, error) {
	ecus, err := d.ECUs(ctx, region, query, 200)
	if err != nil {
		return ECU{}, err
	}
	if len(ecus) == 0 {
		return ECU{}, fmt.Errorf("no ECU matches %q", query)
	}
	// A stable key or category number takes precedence over ambiguous OEM
	// display names such as "EPS" shared by many Toyota generations.
	for _, e := range ecus {
		if strings.EqualFold(e.Key, query) || strconv.Itoa(e.CategoryID) == query {
			return e, nil
		}
	}
	var exact []ECU
	for _, e := range ecus {
		if strings.EqualFold(e.Name, query) {
			exact = append(exact, e)
		}
	}
	if len(exact) == 1 {
		return exact[0], nil
	}
	if len(exact) > 1 {
		ecus = exact
	}
	if len(ecus) != 1 {
		return ECU{}, fmt.Errorf("ambiguous ECU %q: %s", query, joinECUNames(ecus, 8))
	}
	return ecus[0], nil
}

func joinECUNames(ecus []ECU, max int) string {
	names := make([]string, 0, max)
	for i, e := range ecus {
		if i >= max {
			names = append(names, "…")
			break
		}
		names = append(names, fmt.Sprintf("%s (%d)", e.Key, e.CategoryID))
	}
	return strings.Join(names, ", ")
}

func escapeLike(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	return strings.ReplaceAll(s, "_", "\\_")
}

func (d *DB) ECUItems(ctx context.Context, e ECU, kind, query string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := d.db.QueryContext(ctx, `
SELECT kind,region,category_id,identifier,name,detail FROM items
WHERE region=? AND category_id=? AND kind=? AND
(identifier LIKE ? ESCAPE '\' OR name LIKE ? ESCAPE '\' OR detail LIKE ? ESCAPE '\')
ORDER BY identifier COLLATE NOCASE, id LIMIT ?`,
		e.Region, e.CategoryID, kind, pattern, pattern, pattern, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.Kind, &it.Region, &it.CategoryID, &it.Identifier, &it.Name, &it.Detail); err != nil {
			return nil, err
		}
		it.ECU, it.ECUName = e.Key, e.Name
		out = append(out, it)
	}
	return out, rows.Err()
}

func (d *DB) Vehicles(ctx context.Context, region, query string, limit int) ([]Vehicle, error) {
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	if limit <= 0 || limit > 20000 {
		limit = 20000
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := d.db.QueryContext(ctx, `
SELECT region,vehicle_type,name,install_set_ids FROM vehicles
WHERE region=? AND (name LIKE ? ESCAPE '\' OR cast(vehicle_type as text)=?)
ORDER BY CASE WHEN lower(name)=lower(?) THEN 0 ELSE 1 END, name COLLATE NOCASE, vehicle_type LIMIT ?`,
		region, pattern, query, query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Vehicle, 0)
	for rows.Next() {
		var v Vehicle
		var sets string
		if err := rows.Scan(&v.Region, &v.Type, &v.Name, &sets); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(sets), &v.InstallSetIDs); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// VehicleECUs materializes the declared install-set topology with a single
// indexed lookup, independent of the large source resolver index.
func (d *DB) VehicleECUs(ctx context.Context, v Vehicle) ([]VehicleECU, error) {
	rows, err := d.db.QueryContext(ctx, `
SELECT v.category_id, coalesce(e.key, ''), coalesce(e.name, ''),
       v.install_set_id, v.route_selector, v.request_address,
       v.address_extension, coalesce(v.transport_kind, '')
FROM vehicle_ecus v LEFT JOIN ecus e
 ON e.region=v.region AND e.category_id=v.category_id
WHERE v.region=? AND v.vehicle_type=?
ORDER BY v.category_id, v.route_selector, v.install_set_id`, v.Region, v.Type)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]VehicleECU, 0)
	for rows.Next() {
		var item VehicleECU
		var address, extension sql.NullInt64
		if err := rows.Scan(&item.CategoryID, &item.Key, &item.Name,
			&item.InstallSetID, &item.RouteSelector, &address, &extension,
			&item.TransportKind); err != nil {
			return nil, err
		}
		if address.Valid {
			n := int(address.Int64)
			item.RequestAddress = &n
		}
		if extension.Valid && extension.Int64 != 0 {
			n := int(extension.Int64)
			item.AddressExtension = &n
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
