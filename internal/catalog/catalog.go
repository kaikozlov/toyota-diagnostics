// Package catalog reads the indexed SQLite diagnostic catalog.
package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	_ "modernc.org/sqlite"

	"github.com/kaikozlov/toyota-diagnostics/internal/suggest"
)

type DB struct {
	supportMembers        cachedMetadata
	supportQueries        cachedMetadata
	supportFreezeQueries  cachedMetadata
	supportGroups         cachedMetadata
	didMembership         membershipCache
	dtcMembership         membershipCache
	displayMembership     membershipCache
	db                    *sql.DB
	scopes                sync.Map
	Path                  string
	signalRecords         cachedMetadata
	vehicleIndexRecords   cachedMetadata
	topologyRecords       cachedMetadata
	diagnosticCodes       cachedMetadata
	operationDefinitions  cachedMetadata
	operationPlans        cachedMetadata
	behaviorCodes         cachedMetadata
	signalObservations    cachedMetadata
	nativeRecorders       [6]cachedMetadata
	decodedMonitors       [13]cachedMetadata
	legacyFreezeSignals   cachedMetadata
	legacyOperations      cachedMetadata
	legacyConditions      cachedMetadata
	legacyResponses       cachedMetadata
	legacyDynamics        cachedMetadata
	documentCache         cachedDocument
	ecuConfigurationCache cachedDocument
	workStandards         cachedMetadata
	workPatterns          cachedMetadata
	workFactors           cachedMetadata
	decodedStatements     workStatementCache
	workStatements        workStatementCache
	statements            sync.Map
}

type Item struct {
	ID         int64  `json:"id"`
	Kind       string `json:"kind"`
	Region     string `json:"region"`
	CategoryID int    `json:"category_id"`
	ECU        string `json:"ecu"`
	ECUName    string `json:"ecu_name"`
	Identifier string `json:"identifier"`
	Name       string `json:"name"`
	Detail     string `json:"detail"`
}

type ItemDetail struct {
	Item
	Payload json.RawMessage `json:"payload"`
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
	ItemsByKind   map[string]int `json:"items_by_kind,omitempty"`
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
	Release       string `json:"release"`
	DefaultRegion string `json:"default_region"`
	Members       int    `json:"members"`
	Items         int    `json:"items"`
	Vehicles      int    `json:"vehicles"`
	ECUs          int    `json:"ecus"`
}

// ErrLFSPointer reports a checkout whose catalog was never downloaded.
var ErrLFSPointer = errors.New("Git LFS pointer, not the catalog; run git lfs pull in the checkout, or pass --db PATH")

func Open(path string) (*DB, error) {
	if path == "" {
		return nil, errors.New("no catalog configured; pass --db PATH or set TOYO_CATALOG")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var header [64]byte
	n, _ := f.Read(header[:])
	f.Close()
	if strings.HasPrefix(string(header[:n]), "version https://git-lfs.github.com/spec/v1") {
		return nil, fmt.Errorf("%s: %w", path, ErrLFSPointer)
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
	if err := requireTables(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("%s is not a toyo catalog (%v)", abs, err)
	}
	return &DB{db: db, Path: abs}, nil
}

// requiredColumns are the tables and columns every read path depends on. A
// file without them is not a toyo catalog, so Open refuses it up front.
var requiredColumns = map[string]string{
	"meta":                  "key,value",
	"ecus":                  "region,category_id,key,name,database_name,generation,support_mode,catalog_member,counts",
	"item_occurrences":      "id,region_id,category_id,record_id,document_id,pointer_id",
	"search_records":        "id,kind_id,identifier_id,name_id,detail_id",
	"search_kinds":          "id,name",
	"search_regions":        "id,name",
	"text_values":           "id,value",
	"owner_ranges":          "region_id,category_id,first_id,last_id",
	"item_count_rows":       "region_id,category_id,kind_id,count",
	"items_fts":             "rowid,name,detail,identifier",
	"source_documents":      "id,member,data",
	"source_document_parts": "document_id,pointer_id,data",
	"source_pointers":       "id,pointer",
	"vehicle_definitions":   "id,region_id,vehicle_type,name_id,install_count",
}

func requireTables(db *sql.DB) error {
	tables := make([]string, 0, len(requiredColumns))
	for table := range requiredColumns {
		tables = append(tables, table)
	}
	sort.Strings(tables)
	for _, table := range tables {
		rows, err := db.Query("SELECT " + requiredColumns[table] + " FROM " + table + " LIMIT 0")
		if err != nil {
			if strings.Contains(err.Error(), "file is not a database") {
				return errors.New("not an SQLite database")
			}
			return fmt.Errorf("%s: %v", table, err)
		}
		rows.Close()
	}
	return nil
}

// prepared returns a statement prepared once per DB, for queries that run
// repeatedly.
func (d *DB) prepared(ctx context.Context, query string) (*sql.Stmt, error) {
	if stmt, ok := d.statements.Load(query); ok {
		return stmt.(*sql.Stmt), nil
	}
	stmt, err := d.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	if prior, loaded := d.statements.LoadOrStore(query, stmt); loaded {
		stmt.Close()
		return prior.(*sql.Stmt), nil
	}
	return stmt, nil
}

func (d *DB) Close() error {
	if d.db != nil {
		return d.db.Close()
	}
	return nil
}

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
		table string
		dst   *int
	}{{"item_occurrences", &s.Items}, {"vehicle_definitions", &s.Vehicles}, {"ecus", &s.ECUs}} {
		n, err := d.rowCount(ctx, row.table)
		if err != nil {
			return s, err
		}
		*row.dst = n
	}
	return s, nil
}

// rowCount reads the exact count recorded by ANALYZE when the catalog was
// generated; the file is immutable, so it cannot go stale. Counting 200k rows
// takes a third of a second, which is a long time for a status command.
func (d *DB) rowCount(ctx context.Context, table string) (int, error) {
	var stat string
	err := d.db.QueryRowContext(ctx, `SELECT stat FROM sqlite_stat1 WHERE tbl=? LIMIT 1`, table).Scan(&stat)
	if err == nil {
		if n, convErr := strconv.Atoi(strings.Fields(stat)[0]); convErr == nil && n >= 0 {
			return n, nil
		}
	}
	var n int
	if err := d.db.QueryRowContext(ctx, "SELECT count(*) FROM "+table).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func (d *DB) Verify(ctx context.Context) error {
	if err := verifyOwnerRanges(ctx, d.db); err != nil {
		return err
	}
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
// query punctuation cannot be interpreted as MATCH syntax. Each token is a
// quoted prefix term. A bare hex-like token also matches 0x… identifiers;
// an explicit 0x prefix stays in the hex domain and does not match decimal IDs.
func ftsQuery(query string) string {
	words := strings.FieldsFunc(query, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	terms := make([]string, 0, len(words))
	for _, word := range words {
		terms = append(terms, ftsTerm(word))
	}
	return strings.Join(terms, " AND ")
}

func ftsTerm(word string) string {
	word = strings.ToLower(word)
	quoted := func(s string) string { return `"` + s + `"*` }
	explicit := strings.HasPrefix(word, "0x")
	bare, ok := hexBare(word)
	if !ok {
		return quoted(word)
	}
	if explicit {
		return quoted(word)
	}
	return "(" + quoted(word) + " OR " + quoted("0x"+bare) + ")"
}

// normalizeIdent folds hex DID spellings so 0x1601, 1601 and 0X1601 agree.
func normalizeIdent(s string) string {
	return strings.TrimPrefix(strings.ToLower(strings.TrimSpace(s)), "0x")
}

// hexBare reports a 3–8 digit hex identifier, after stripping a leading 0x.
// Shorter tokens stay ordinary words so "be" or "a" are not treated as DIDs.
// That also means search does not treat "22" and "0x22" as the same DID:
// two hex digits are below the floor. Leading zeros are kept ("01601" is
// not "1601"). English words that are hex (ace, dead) still expand.
func hexBare(s string) (string, bool) {
	s = normalizeIdent(s)
	if len(s) < 3 || len(s) > 8 {
		return "", false
	}
	for _, r := range s {
		if !unicode.Is(unicode.ASCII_Hex_Digit, r) {
			return "", false
		}
	}
	return s, true
}

// queryIdentForm is the hex digit string in q if there is exactly one such
// token, even when punctuation surrounded it ("(0x1601)"). explicit is true
// when that token was written with a 0x prefix. Conflicting hex tokens cancel.
func queryIdentForm(q string) (bare string, explicit bool) {
	trimmed := strings.TrimSpace(q)
	if b, ok := hexBare(trimmed); ok {
		return b, strings.HasPrefix(strings.ToLower(trimmed), "0x")
	}
	found := ""
	for _, word := range strings.FieldsFunc(q, func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		b, ok := hexBare(word)
		if !ok {
			continue
		}
		if found != "" && found != b {
			return "", false
		}
		found = b
		if strings.HasPrefix(strings.ToLower(word), "0x") {
			explicit = true
		}
	}
	return found, explicit
}

func (d *DB) Search(ctx context.Context, region, query string, limit int) ([]Item, error) {
	return d.search(ctx, region, 0, "", query, limit)
}

// SearchECU is Search confined to one ECU category.
func (d *DB) SearchECU(ctx context.Context, e ECU, query string, limit int) ([]Item, error) {
	return d.search(ctx, e.Region, e.CategoryID, "", query, limit)
}

// SearchKind is Search confined to one record kind, optionally within an ECU
// (category 0 means every ECU).
func (d *DB) SearchKind(ctx context.Context, region string, category int, kind, query string, limit int) ([]Item, error) {
	return d.search(ctx, region, category, kind, query, limit)
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
	var rawCounts map[string]json.RawMessage
	if err := json.Unmarshal([]byte(counts), &rawCounts); err != nil {
		return e, err
	}
	e.Counts = make(map[string]int)
	for key, raw := range rawCounts {
		if key == "items_by_kind" {
			if err := json.Unmarshal(raw, &e.ItemsByKind); err != nil {
				return e, err
			}
			continue
		}
		var count int
		if err := json.Unmarshal(raw, &count); err != nil {
			return e, err
		}
		e.Counts[key] = count
	}
	return e, nil
}

const selectECUs = `SELECT region,category_id,key,name,database_name,generation,support_mode,catalog_member,counts FROM ecus`

func (d *DB) ECUs(ctx context.Context, region, query string, limit int) ([]ECU, error) {
	return d.ListECUs(ctx, region, query, false, limit)
}

// ListECUs matches by key, name or category. An exact key always leads, and
// ECUs with a decoded catalog precede the declaration-only ones, which are
// numerous and rarely what a person is looking for.
func (d *DB) ListECUs(ctx context.Context, region, query string, decodedOnly bool, limit int) ([]ECU, error) {
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
  AND (?=0 OR catalog_member IS NOT NULL)
ORDER BY CASE WHEN lower(key)=lower(?) THEN 0 ELSE 1 END,
         CASE WHEN catalog_member IS NULL THEN 1 ELSE 0 END,
         key COLLATE NOCASE, category_id LIMIT ?`,
		region, pattern, pattern, query, decodedOnly, query, limit)
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

// NotFoundError reports a lookup that matched nothing, with near misses.
type NotFoundError struct {
	What        string
	Query       string
	Suggestions []string
}

func (e *NotFoundError) Error() string {
	switch e.What {
	case "":
		return fmt.Sprintf("nothing matches %q", e.Query)
	case "command":
		return fmt.Sprintf("unknown command %q", e.Query)
	}
	return fmt.Sprintf("no %s matches %q", e.What, e.Query)
}

// AmbiguousError reports a lookup that matched several records equally well.
type AmbiguousError struct {
	What    string
	Query   string
	Matches []ECU
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("ambiguous %s %q: %s", e.What, e.Query, joinECUNames(e.Matches, 8))
}

// ECUNames lists every ECU key and display name, for spelling suggestions.
func (d *DB) ECUNames(ctx context.Context, region string) ([]string, error) {
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	rows, err := d.db.QueryContext(ctx, `SELECT key, name FROM ecus WHERE region=? ORDER BY catalog_member IS NULL, key`, region)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var key, name string
		if err := rows.Scan(&key, &name); err != nil {
			return nil, err
		}
		out = append(out, key, name)
	}
	return out, rows.Err()
}

// suggestECUs proposes ECU keys for a misspelled key or display name.
func (d *DB) suggestECUs(ctx context.Context, region, query string) []string {
	pairs, err := d.ECUNames(ctx, region)
	if err != nil {
		return nil
	}
	keyOf := map[string]string{}
	pool := make([]string, 0, len(pairs))
	for i := 0; i+1 < len(pairs); i += 2 {
		key, name := pairs[i], pairs[i+1]
		keyOf[key] = key
		if _, ok := keyOf[name]; !ok {
			keyOf[name] = key
		}
		pool = append(pool, key, name)
	}
	var out []string
	seen := map[string]bool{}
	for _, hit := range suggest.Closest(query, pool, 12) {
		key := keyOf[hit]
		if !seen[key] {
			seen[key] = true
			out = append(out, key)
		}
		if len(out) == 4 {
			break
		}
	}
	return out
}

// ExactECU reports an ECU only when query is its key, category id, or full
// display name (case-insensitive). Unlike ECU, a unique substring is not
// enough, so "Front" or "2" do not resolve Front Recognition Camera 2.
func (d *DB) ExactECU(ctx context.Context, region, query string) (ECU, bool, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return ECU{}, false, nil
	}
	region, err := Region(region)
	if err != nil {
		return ECU{}, false, err
	}
	rows, err := d.db.QueryContext(ctx, selectECUs+` WHERE region=? AND (lower(key)=lower(?) OR cast(category_id as text)=? OR lower(name)=lower(?))`,
		region, query, query, query)
	if err != nil {
		return ECU{}, false, err
	}
	defer rows.Close()
	var hit []ECU
	for rows.Next() {
		row, err := scanECU(rows)
		if err != nil {
			return ECU{}, false, err
		}
		hit = append(hit, row)
	}
	if err := rows.Err(); err != nil {
		return ECU{}, false, err
	}
	for _, e := range hit {
		if strings.EqualFold(e.Key, query) || strconv.Itoa(e.CategoryID) == query {
			return e, true, nil
		}
	}
	if len(hit) == 1 {
		return hit[0], true, nil
	}
	return ECU{}, false, nil
}

func (d *DB) ECU(ctx context.Context, region, query string) (ECU, error) {
	ecus, err := d.ECUs(ctx, region, query, 200)
	if err != nil {
		return ECU{}, err
	}
	if len(ecus) == 0 {
		return ECU{}, &NotFoundError{What: "ECU", Query: query, Suggestions: d.suggestECUs(ctx, region, query)}
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
		return ECU{}, &AmbiguousError{What: "ECU", Query: query, Matches: ecus}
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

// ECUItems lists one family of an ECU's records. Without a query they come
// in identifier order. With one, words match from the start ("steer" finds
// "Steering") and the best match leads; only when that finds nothing does a
// plain substring match run, so "volt" still finds "Undervoltage".
func (d *DB) ECUItems(ctx context.Context, e ECU, kind, query string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	if strings.TrimSpace(query) != "" {
		ranked, err := d.search(ctx, e.Region, e.CategoryID, kind, query, limit)
		if err != nil {
			return nil, err
		}
		if len(ranked) > 0 {
			return ranked, nil
		}
	}
	pattern := "%" + escapeLike(query) + "%"
	rows, err := d.db.QueryContext(ctx, `
SELECT id,kind,region,category_id,identifier,name,detail FROM owner_items
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
		if err := rows.Scan(&it.ID, &it.Kind, &it.Region, &it.CategoryID, &it.Identifier, &it.Name, &it.Detail); err != nil {
			return nil, err
		}
		it.ECU, it.ECUName = e.Key, e.Name
		out = append(out, it)
	}
	return out, rows.Err()
}

// ItemsByIdentifier finds an ECU's records by DID, DTC or test identifier.
// Hex is forgiving: 0x1601, 1601 and 0X1601 are the same identifier.
func (d *DB) ItemsByIdentifier(ctx context.Context, e ECU, ident string) ([]Item, error) {
	want := normalizeIdent(ident)
	if want == "" {
		return nil, nil
	}
	rows, err := d.db.QueryContext(ctx, `
SELECT id,kind,region,category_id,identifier,name,detail FROM owner_items
WHERE region=? AND category_id=? AND kind NOT IN ('ecu','vehicle')
  AND replace(lower(identifier),'0x','')=?
ORDER BY kind, id`, e.Region, e.CategoryID, want)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Kind, &it.Region, &it.CategoryID, &it.Identifier, &it.Name, &it.Detail); err != nil {
			return nil, err
		}
		it.ECU, it.ECUName = e.Key, e.Name
		out = append(out, it)
	}
	return out, rows.Err()
}

// VehicleGroup collapses Toyota's per-generation vehicle types that share an
// OEM name; the type IDs remain the precise handles.
type VehicleGroup struct {
	Name  string `json:"name"`
	Types []int  `json:"vehicle_types"`
}

// GroupVehicles preserves the order of first appearance.
func GroupVehicles(vehicles []Vehicle) []VehicleGroup {
	index := map[string]int{}
	var out []VehicleGroup
	for _, v := range vehicles {
		key := strings.ToLower(v.Name)
		i, ok := index[key]
		if !ok {
			i = len(out)
			index[key] = i
			out = append(out, VehicleGroup{Name: v.Name})
		}
		out[i].Types = append(out[i].Types, v.Type)
	}
	return out
}

// ECURecords lists an ECU's records without a search term: one kind, or all
// kinds (grouped by kind, then identifier) when kind is empty.
func (d *DB) ECURecords(ctx context.Context, e ECU, kind string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	rows, err := d.db.QueryContext(ctx, `
SELECT id,kind,region,category_id,identifier,name,detail FROM owner_items
WHERE region=? AND category_id=? AND kind NOT IN ('ecu','vehicle') AND (?='' OR kind=?)
ORDER BY kind, identifier COLLATE NOCASE, id LIMIT ?`, e.Region, e.CategoryID, kind, kind, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Kind, &it.Region, &it.CategoryID, &it.Identifier, &it.Name, &it.Detail); err != nil {
			return nil, err
		}
		it.ECU, it.ECUName = e.Key, e.Name
		out = append(out, it)
	}
	return out, rows.Err()
}

// ListItems browses catalog records without a full-text query. category 0
// means every ECU; an empty kind means every kind.
func (d *DB) ListItems(ctx context.Context, region string, category int, kind string, limit int) ([]Item, error) {
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	region, err := Region(region)
	if err != nil {
		return nil, err
	}
	if kind == "" {
		// Kind is the leading sort key. Read only enough ordered families to
		// fill the page instead of sorting every record in the region.
		kinds, err := d.Kinds(ctx, region, category)
		if err != nil {
			return nil, err
		}
		out := make([]Item, 0)
		for _, family := range kinds {
			if family == "" {
				return nil, fmt.Errorf("catalog contains an empty record kind")
			}
			rows, err := d.ListItems(ctx, region, category, family, limit-len(out))
			if err != nil {
				return nil, err
			}
			out = append(out, rows...)
			if len(out) >= limit {
				break
			}
		}
		return out, nil
	}
	query := `SELECT i.id,i.kind,i.region,i.category_id,coalesce(e.key,''),coalesce(e.name,''),i.identifier,i.name,i.detail
FROM owner_items i LEFT JOIN ecus e ON e.region=i.region AND e.category_id=i.category_id
WHERE i.region=?`
	args := []any{region}
	if category != 0 {
		query += " AND i.category_id=?"
		args = append(args, category)
	}
	if kind != "" {
		query += " AND i.kind=?"
		args = append(args, kind)
		if category == 0 {
			query += kindOwnerPredicate
			args = append(args, region, kind)
		}
	}
	query += " ORDER BY i.kind,i.identifier COLLATE NOCASE,i.id LIMIT ?"
	args = append(args, limit)
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Item, 0)
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Kind, &it.Region, &it.CategoryID, &it.ECU, &it.ECUName, &it.Identifier, &it.Name, &it.Detail); err != nil {
			return nil, err
		}
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

// VehicleECUCounts maps each vehicle type to how many distinct ECUs its
// install sets declare, in one pass.
func (d *DB) VehicleECUCounts(ctx context.Context, region string) (map[int]int, error) {
	rows, err := d.db.QueryContext(ctx, `SELECT vehicle_type,ecu_count FROM vehicle_ecu_counts WHERE region=?`, region)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]int{}
	for rows.Next() {
		var vt, n int
		if err := rows.Scan(&vt, &n); err != nil {
			return nil, err
		}
		out[vt] = n
	}
	return out, rows.Err()
}

// Item retrieves the source record behind a search result. IDs belong to the
// current catalog; callers should not carry them across catalog replacements.
func (d *DB) Item(ctx context.Context, id int64) (ItemDetail, error) {
	var result ItemDetail
	err := d.db.QueryRowContext(ctx, `SELECT id,kind,region,category_id,identifier,name,detail
 FROM items WHERE id=?`, id).Scan(&result.ID, &result.Kind, &result.Region, &result.CategoryID,
		&result.Identifier, &result.Name, &result.Detail)
	if errors.Is(err, sql.ErrNoRows) {
		return result, fmt.Errorf("no catalog item %d", id)
	}
	if err != nil {
		return result, err
	}
	// A LEFT JOIN to a derived ECU view can materialize every ECU before
	// selecting one item. This owner lookup stays indexed.
	err = d.db.QueryRowContext(ctx, `SELECT key,name FROM ecus WHERE region=? AND category_id=?`, result.Region, result.CategoryID).Scan(&result.ECU, &result.ECUName)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return result, err
	}
	payload, err := d.itemPayloadBytes(ctx, id)
	if err != nil {
		return result, fmt.Errorf("invalid payload for catalog item %d: %w", id, err)
	}
	result.Payload = json.RawMessage(payload)
	return result, nil
}
