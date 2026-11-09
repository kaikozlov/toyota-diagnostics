// Package catalogtest builds small catalogs with the shipped database schema
// for tests in any package.
package catalogtest

import (
	"database/sql"
	_ "embed"
	"encoding/json"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/klauspost/compress/zstd"
	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var Schema string

// ItemsMember holds payloads for items added without their own document.
const ItemsMember = "items.json"

type ECU struct {
	Region        string
	Category      int
	Key, Name     string
	Database      string
	Generation    int // zero is stored as NULL
	SupportMode   string
	CatalogMember string
	Counts        string // JSON; empty means {}
}

// Item is one searchable record. Without Member its Payload is stored in
// ItemsMember at /<ID>; with Member, Pointer locates it in that document.
type Item struct {
	ID                             int64
	Region                         string
	Category                       int
	Kind, Identifier, Name, Detail string
	Payload                        any
	Member, Pointer                string
}

// Route is one transport route; zero response/legislated addresses are NULL/0.
type Route struct {
	Region                             string
	Category, Selector                 int
	Request, Response, Legislated, Ext int
	Transport                          string
}

type Catalog struct {
	t        testing.TB
	db       *sql.DB
	path     string
	payloads map[string]any
	closed   bool
}

func New(t testing.TB) *Catalog {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	// Fixtures are disposable; skip durability so each insert is not a sync.
	db, err := sql.Open("sqlite", path+"?_pragma=synchronous(off)&_pragma=journal_mode(off)")
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	c := &Catalog{t: t, db: db, path: path, payloads: map[string]any{}}
	t.Cleanup(func() {
		if !c.closed {
			db.Close()
		}
	})
	c.Exec(Schema)
	c.Exec(`INSERT INTO meta VALUES('release','test'),('default_region','NA')`)
	return c
}

func (c *Catalog) Exec(query string, args ...any) {
	c.t.Helper()
	if _, err := c.db.Exec(query, args...); err != nil {
		c.t.Fatalf("%v\n%s", err, query)
	}
}

func (c *Catalog) id(query string, args ...any) int64 {
	c.t.Helper()
	var id int64
	if err := c.db.QueryRow(query, args...).Scan(&id); err != nil {
		c.t.Fatal(err)
	}
	return id
}

func (c *Catalog) text(value string) int64 {
	c.Exec(`INSERT OR IGNORE INTO text_values(value) VALUES(?)`, value)
	return c.id(`SELECT id FROM text_values WHERE value=?`, value)
}

func (c *Catalog) named(table, name string) int64 {
	c.Exec(`INSERT OR IGNORE INTO `+table+`(name) VALUES(?)`, name)
	return c.id(`SELECT id FROM `+table+` WHERE name=?`, name)
}

func (c *Catalog) pointer(pointer string) int64 {
	c.Exec(`INSERT OR IGNORE INTO source_pointers(pointer) VALUES(?)`, pointer)
	return c.id(`SELECT id FROM source_pointers WHERE pointer=?`, pointer)
}

func (c *Catalog) document(member string) int64 {
	c.Exec(`INSERT OR IGNORE INTO source_documents(member,data) VALUES(?,x'')`, member)
	return c.id(`SELECT id FROM source_documents WHERE member=?`, member)
}

// Pack compresses a JSON value the way the shipped catalog stores documents.
func Pack(t testing.TB, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	enc, err := zstd.NewWriter(nil)
	if err != nil {
		t.Fatal(err)
	}
	defer enc.Close()
	return enc.EncodeAll(raw, nil)
}

// Document stores a source document under member.
func (c *Catalog) Document(member string, value any) {
	c.t.Helper()
	c.document(member)
	c.Exec(`UPDATE source_documents SET data=? WHERE member=?`, Pack(c.t, value), member)
}

func (c *Catalog) ECU(e ECU) {
	c.t.Helper()
	if e.Region == "" {
		e.Region = "NA"
	}
	if e.Counts == "" {
		e.Counts = "{}"
	}
	var generation, member, mode any
	if e.Generation != 0 {
		generation = e.Generation
	}
	if e.CatalogMember != "" {
		member = e.CatalogMember
	}
	if e.SupportMode != "" {
		mode = e.SupportMode
	}
	c.Exec(`INSERT INTO ecus VALUES(?,?,?,?,?,?,?,?,?)`, e.Region, e.Category, e.Key, e.Name, e.Database, generation, mode, member, e.Counts)
}

func (c *Catalog) Item(items ...Item) {
	c.t.Helper()
	for _, it := range items {
		if it.Region == "" {
			it.Region = "NA"
		}
		if it.Member == "" {
			it.Member, it.Pointer = ItemsMember, "/"+strconv.FormatInt(it.ID, 10)
			payload := it.Payload
			if payload == nil {
				payload = map[string]any{}
			}
			c.payloads[strconv.FormatInt(it.ID, 10)] = payload
		}
		kind := c.named("search_kinds", it.Kind)
		region := c.named("search_regions", it.Region)
		ident, name, detail := c.text(it.Identifier), c.text(it.Name), c.text(it.Detail)
		c.Exec(`INSERT OR IGNORE INTO search_records(kind_id,identifier_id,name_id,detail_id) VALUES(?,?,?,?)`, kind, ident, name, detail)
		record := c.id(`SELECT id FROM search_records WHERE kind_id=? AND identifier_id=? AND name_id=? AND detail_id=?`, kind, ident, name, detail)
		c.Exec(`INSERT INTO item_occurrences VALUES(?,?,?,?,?,?)`, it.ID, region, it.Category, record, c.document(it.Member), c.pointer(it.Pointer))
	}
}

// Vehicle declares a vehicle type and its ordered install sets.
func (c *Catalog) Vehicle(region string, vehicleType int, name string, installSets ...int) {
	c.t.Helper()
	regionID := c.named("search_regions", region)
	c.Exec(`INSERT INTO vehicle_definitions(region_id,vehicle_type,name_id,install_count) VALUES(?,?,?,?)`, regionID, vehicleType, c.text(name), len(installSets))
	vehicle := c.id(`SELECT id FROM vehicle_definitions WHERE region_id=? AND vehicle_type=?`, regionID, vehicleType)
	for i, set := range installSets {
		c.Exec(`INSERT INTO vehicle_install_references VALUES(?,?,?)`, vehicle, i, set)
	}
}

func (c *Catalog) Route(r Route) {
	c.t.Helper()
	if r.Region == "" {
		r.Region = "NA"
	}
	var response any
	if r.Response != 0 {
		response = r.Response
	}
	c.Exec(`INSERT INTO vehicle_route_definitions(address_extension,change_comm_if_case,functional_address,functional_address_field,legislated_request_address,protocol_info_id,request_address,request_address_field,request_mask,response_mask,route_selector,physical_request_address,physical_response_address,transport_kind_id) VALUES(?,0,0,0,?,0,?,?,4095,4095,?,?,?,?)`,
		r.Ext, r.Legislated, r.Request, r.Request, r.Selector, r.Request, response, c.text(r.Transport))
	definition := c.id(`SELECT max(id) FROM vehicle_route_definitions`)
	c.Exec(`INSERT INTO vehicle_route_owners VALUES(?,?,?,?)`, c.named("search_regions", r.Region), r.Category, r.Selector, definition)
}

// Install adds one ECU connection to an install set; routed links it to the
// Route with the same category and selector.
func (c *Catalog) Install(region string, set, category, selector, frame int, routed bool) {
	c.t.Helper()
	regionID := c.named("search_regions", region)
	has := 0
	if routed {
		has = 1
	}
	c.Exec(`INSERT INTO vehicle_connections(category_id,comm_set_id,frame_id,route_selector,has_route) VALUES(?,0,?,?,?)`, category, frame, selector, has)
	connection := c.id(`SELECT max(id) FROM vehicle_connections`)
	var definition int64
	if err := c.db.QueryRow(`SELECT definition_id FROM install_set_owners WHERE region_id=? AND install_set_id=?`, regionID, set).Scan(&definition); err == sql.ErrNoRows {
		c.Exec(`INSERT INTO install_set_definitions(member_count) VALUES(0)`)
		definition = c.id(`SELECT max(id) FROM install_set_definitions`)
		c.Exec(`INSERT INTO install_set_owners VALUES(?,?,?)`, regionID, set, definition)
	} else if err != nil {
		c.t.Fatal(err)
	}
	position := c.id(`SELECT member_count FROM install_set_definitions WHERE id=?`, definition)
	c.Exec(`INSERT INTO install_set_members VALUES(?,?,?)`, definition, position, connection)
	c.Exec(`UPDATE install_set_definitions SET member_count=member_count+1 WHERE id=?`, definition)
}

// Path finishes the derived indexes and closes the writer.
func (c *Catalog) Path() string {
	c.t.Helper()
	if c.closed {
		return c.path
	}
	if len(c.payloads) > 0 {
		c.Document(ItemsMember, c.payloads)
	}
	c.Exec(`INSERT OR REPLACE INTO meta VALUES('member_count',(SELECT count(*) FROM source_documents))`)
	c.Exec(`INSERT INTO owner_ranges SELECT region_id,category_id,min(id),max(id) FROM item_occurrences GROUP BY region_id,category_id`)
	c.Exec(`INSERT INTO item_count_rows SELECT o.region_id,o.category_id,r.kind_id,count(*) FROM item_occurrences o JOIN search_records r ON r.id=o.record_id GROUP BY 1,2,3`)
	c.Exec(`INSERT INTO items_fts(items_fts) VALUES('rebuild')`)
	if err := c.db.Close(); err != nil {
		c.t.Fatal(err)
	}
	c.closed = true
	return c.path
}
