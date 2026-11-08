package catalog

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func fixture(t *testing.T) *DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "catalog.sqlite")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := `
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta VALUES ('schema','toyota-diagnostics-sqlite-v2'),('source_schema','toyota-diagnostics-bundle-v4'),('release','test'),('default_region','NA'),('member_count','2');
CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);
INSERT INTO ecus VALUES('NA',498,'frc','Front Recognition Camera 2','FRC_P5.ddb',20,'p5-standard','catalogs/NA/498.json','{"dids":1,"signals":2}');
INSERT INTO ecus VALUES('NA',405,'eps','Power Steering','EMPS_P5.ddb',20,'p5-standard',null,'{}');
INSERT INTO ecus VALUES('NA',317,'eps_317','EPS','EPS_P4.ddb',4,'p4',null,'{}');
CREATE TABLE vehicles (region TEXT,vehicle_type INTEGER,name TEXT,install_set_ids TEXT);
CREATE TABLE vehicle_ecus (region TEXT, vehicle_type INTEGER, install_set_id INTEGER, category_id INTEGER, route_selector INTEGER, connection_frame_id INTEGER, route_key TEXT, request_address INTEGER, address_extension INTEGER, transport_kind TEXT);
INSERT INTO vehicle_ecus VALUES('NA',12704,8119,498,18,0,'498:18',1938,0,'iso15765-phase-family');
INSERT INTO vehicles VALUES('NA',12704,'Camry HV','[8119,8120]');
INSERT INTO vehicles VALUES('NA',12999,'Camry','[3]');
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT,payload TEXT);
INSERT INTO items VALUES(1,'NA',498,'did','0x1601','LTA Control Condition','','{"name":"LTA Control Condition","bit_start":3,"unit":"deg"}');
INSERT INTO items VALUES(2,'NA',498,'did','0x1601','LTA Switch Condition Flag','','{}');
INSERT INTO items VALUES(3,'NA',498,'ffd-signal','0x5285','Arbitration result_lateral ID','TSS3 Operation FFD','{}');
INSERT INTO items VALUES(4,'NA',0,'adu-signal','0x1593','Arbitration result Lateral ID','ADAS recorder','{}');
INSERT INTO items VALUES(5,'NA',0,'did','0xFFFF','Mentions 1601 only in the title','also 1601 in detail','{}');
CREATE VIRTUAL TABLE items_fts USING fts5(name,detail,identifier,content='items',content_rowid='id',tokenize='unicode61',prefix='2 3 4');
INSERT INTO items_fts(items_fts) VALUES('rebuild');
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE item_payloads(item_id INTEGER PRIMARY KEY,data BLOB)"); err != nil {
		t.Fatal(err)
	}
	for id, payload := range map[int]string{1: `{"name":"LTA Control Condition","bit_start":3,"unit":"deg"}`, 2: `{}`, 3: `{}`, 4: `{}`, 5: `{}`} {
		var buffer bytes.Buffer
		writer := zlib.NewWriter(&buffer)
		if _, err := writer.Write([]byte(payload)); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO item_payloads VALUES (?,?)", id, buffer.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestFTSAndIndependentRecorderNamespaces(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	res, err := d.Search(ctx, "na", "Arbitration result lateral ID", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 2 {
		t.Fatalf("expected TSS3 and ADU sources, got %#v", res)
	}
	kinds := map[string]bool{}
	for _, r := range res {
		kinds[r.Kind] = true
	}
	if !kinds["ffd-signal"] || !kinds["adu-signal"] {
		t.Fatal(kinds)
	}
	lta, err := d.Search(ctx, "NA", "LTA", 50)
	if err != nil || len(lta) != 2 {
		t.Fatalf("did aliases lost: %#v %v", lta, err)
	}
	empty, err := d.Search(ctx, "NA", `" OR 1=1 --`, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(empty) != 0 {
		t.Fatal(empty)
	}
	if _, err := d.Search(ctx, "XX", "LTA", 10); err == nil {
		t.Fatal("unknown region accepted")
	}
}

func TestCategoryLookupAndSourcePreservation(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	e, err := d.ECU(ctx, "NA", "FRC")
	if err != nil {
		t.Fatal(err)
	}
	if e.CategoryID != 498 || e.Counts["signals"] != 2 {
		t.Fatalf("%+v", e)
	}
	primary, err := d.ECU(ctx, "NA", "eps")
	if err != nil || primary.CategoryID != 405 {
		t.Fatalf("primary EPS lookup: %+v %v", primary, err)
	}
	items, err := d.ECUItems(ctx, e, "did", "control", 10)
	if err != nil || len(items) != 1 || items[0].Identifier != "0x1601" {
		t.Fatalf("%v %v", items, err)
	}
	if _, err := d.ECU(ctx, "NA", "missing"); err == nil {
		t.Fatal("unknown ECU accepted")
	}
	vehicles, err := d.Vehicles(ctx, "NA", "12704", 10)
	if err != nil || len(vehicles) != 1 || vehicles[0].Name != "Camry HV" {
		t.Fatal(vehicles, err)
	}
	candidates, err := d.VehicleECUs(ctx, vehicles[0])
	if err != nil || len(candidates) != 1 || *candidates[0].RequestAddress != 0x792 {
		t.Fatal(candidates, err)
	}
	s, err := d.Stats(ctx)
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(s)
	if !strings.Contains(string(encoded), `"source_schema":"toyota-diagnostics-bundle-v4"`) {
		t.Fatal(string(encoded))
	}
	if err := d.Verify(ctx); err != nil {
		t.Fatal(err)
	}
}

func TestInvalidSQLiteSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE meta(key TEXT,value TEXT); INSERT INTO meta VALUES('schema','bad')"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, err := Open(path); err == nil || !strings.Contains(err.Error(), "schema") {
		t.Fatal(err)
	}
}

func TestSourceRecordLookup(t *testing.T) {
	d := fixture(t)
	item, err := d.Item(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if item.ID != 1 || item.Identifier != "0x1601" || !strings.Contains(string(item.Payload), `"bit_start":3`) {
		t.Fatalf("%+v", item)
	}
	if _, err := d.Item(context.Background(), 999); err == nil {
		t.Fatal("missing item accepted")
	}
	rows, err := d.Search(context.Background(), "NA", "LTA", 20)
	if err != nil || len(rows) != 2 || rows[0].ID == 0 {
		t.Fatal(rows, err)
	}
}
func TestSingleCharacterAndEmptyQueries(t *testing.T) {
	d := fixture(t)
	if ftsQuery("x") != `"x"*` {
		t.Fatal("single-letter search discarded")
	}
	if ftsQuery("LTA") != `"lta"*` {
		t.Fatalf("LTA: %q", ftsQuery("LTA"))
	}
	if ftsQuery("1601") != `("1601"* OR "0x1601"*)` {
		t.Fatalf("1601: %q", ftsQuery("1601"))
	}
	if ftsQuery("0x1601") != `("0x1601"* OR "1601"*)` {
		t.Fatalf("0x1601: %q", ftsQuery("0x1601"))
	}
	if ftsQuery("LTA 1601") != `"lta"* AND ("1601"* OR "0x1601"*)` {
		t.Fatalf("multiword: %q", ftsQuery("LTA 1601"))
	}
	rows, err := d.Search(context.Background(), "NA", "---", 20)
	if err != nil || rows == nil || len(rows) != 0 {
		t.Fatal(rows, err)
	}
}

func TestSearchRanksExactIdentifiersBeforeTextMentions(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	for _, q := range []string{"1601", "0x1601", "0X1601", " 0x1601 "} {
		rows, err := d.Search(ctx, "NA", q, 10)
		if err != nil {
			t.Fatal(q, err)
		}
		if len(rows) < 3 {
			t.Fatalf("%q: %#v", q, rows)
		}
		if rows[0].Identifier != "0x1601" || rows[1].Identifier != "0x1601" {
			t.Fatalf("%q should list DID 0x1601 first: %#v", q, rows)
		}
		var sawMention bool
		for i, row := range rows {
			if row.Identifier == "0xFFFF" {
				sawMention = true
				if i < 2 {
					t.Fatalf("%q ranked a title mention of 1601 above the DID: %#v", q, rows)
				}
			}
		}
		if !sawMention {
			t.Fatalf("%q should still find the title mention: %#v", q, rows)
		}
	}
}

func TestSearchQueryPunctuationDoesNotBecomeOperators(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	for _, q := range []string{`" OR 1=1 --`, `NOT LTA`, `name:LTA`, `NEAR(LTA, 2)`, `LTA*`, `0x1601; DROP TABLE items`} {
		rows, err := d.Search(ctx, "NA", q, 10)
		if err != nil {
			t.Fatalf("%q should be a safe MATCH parameter: %v", q, err)
		}
		_ = rows
	}
	// A real identifier still wins after surrounding punctuation is stripped.
	rows, err := d.Search(ctx, "NA", "(0x1601)", 10)
	if err != nil || len(rows) == 0 || rows[0].Identifier != "0x1601" {
		t.Fatalf("punctuated identifier: %#v %v", rows, err)
	}
	// No silent rewrite of a typo into a nearby catalog word.
	miss, err := d.Search(ctx, "NA", "LTX", 10)
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range miss {
		if strings.Contains(row.Name, "LTA") {
			t.Fatalf("LTX should not be rewritten into LTA: %#v", miss)
		}
	}
}

func TestIdentifierLookupIsForgivingAboutHex(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	frc, err := d.ECU(ctx, "NA", "frc")
	if err != nil {
		t.Fatal(err)
	}
	for _, ident := range []string{"0x1601", "1601", "0X1601", " 0x1601 "} {
		items, err := d.ItemsByIdentifier(ctx, frc, ident)
		if err != nil || len(items) != 2 || items[0].ECU != "frc" {
			t.Fatalf("%q: %v %v", ident, items, err)
		}
	}
	if items, _ := d.ItemsByIdentifier(ctx, frc, "0x9999"); len(items) != 0 {
		t.Fatal(items)
	}
	if items, _ := d.ItemsByIdentifier(ctx, frc, ""); len(items) != 0 {
		t.Fatal("empty identifier matched", items)
	}
}

func TestMissingECUComesWithSuggestions(t *testing.T) {
	d := fixture(t)
	_, err := d.ECU(context.Background(), "NA", "frcc")
	var miss *NotFoundError
	if !errors.As(err, &miss) {
		t.Fatalf("want NotFoundError, got %v", err)
	}
	if len(miss.Suggestions) == 0 || miss.Suggestions[0] != "frc" {
		t.Fatalf("suggestions: %v", miss.Suggestions)
	}
	if !strings.Contains(err.Error(), `no ECU matches "frcc"`) {
		t.Fatal(err)
	}
}

func TestAmbiguousECUCarriesCandidates(t *testing.T) {
	d := fixture(t)
	_, err := d.ECU(context.Background(), "NA", "EPS")
	// Key "eps" wins over the shared display name, so this resolves.
	if err != nil {
		t.Fatal(err)
	}
	_, err = d.ECU(context.Background(), "NA", "Steering")
	var amb *AmbiguousError
	if err != nil && !errors.As(err, &amb) {
		t.Fatalf("unexpected error type: %v", err)
	}
}

func TestListECUsLeadsWithDecodedCatalogs(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	all, err := d.ListECUs(ctx, "NA", "", false, 10)
	if err != nil || len(all) != 3 || all[0].CatalogMember == "" {
		t.Fatalf("decoded ECUs should lead: %+v %v", all, err)
	}
	decoded, err := d.ListECUs(ctx, "NA", "", true, 10)
	if err != nil || len(decoded) != 1 || decoded[0].Key != "frc" {
		t.Fatalf("%+v %v", decoded, err)
	}
}

func TestSearchScopesByECUAndKind(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	frc, _ := d.ECU(ctx, "NA", "frc")
	scoped, err := d.SearchECU(ctx, frc, "lta", 10)
	if err != nil || len(scoped) != 2 {
		t.Fatalf("%v %v", scoped, err)
	}
	others, err := d.SearchKind(ctx, "NA", 0, "adu-signal", "lateral", 10)
	if err != nil || len(others) != 1 || others[0].Kind != "adu-signal" {
		t.Fatalf("%v %v", others, err)
	}
	none, err := d.SearchKind(ctx, "NA", 0, "dtc", "lta", 10)
	if err != nil || len(none) != 0 {
		t.Fatalf("%v %v", none, err)
	}
	records, err := d.ECURecords(ctx, frc, "", 10)
	if err != nil || len(records) != 3 {
		t.Fatalf("%v %v", records, err)
	}
	onlyDID, err := d.ECURecords(ctx, frc, "did", 10)
	if err != nil || len(onlyDID) != 2 {
		t.Fatalf("%v %v", onlyDID, err)
	}
}

func TestRankedECUItemSearchFallsBackToSubstring(t *testing.T) {
	d := fixture(t)
	ctx := context.Background()
	frc, _ := d.ECU(ctx, "NA", "frc")
	prefix, err := d.ECUItems(ctx, frc, "did", "cont", 10)
	if err != nil || len(prefix) != 1 {
		t.Fatalf("prefix search: %v %v", prefix, err)
	}
	// "ondition" starts no word; only the substring fallback finds it.
	sub, err := d.ECUItems(ctx, frc, "did", "ondition", 10)
	if err != nil || len(sub) != 2 {
		t.Fatalf("substring fallback: %v %v", sub, err)
	}
	all, err := d.ECUItems(ctx, frc, "did", "", 10)
	if err != nil || len(all) != 2 {
		t.Fatalf("no query lists everything: %v %v", all, err)
	}
}

func TestGroupVehiclesKeepsOrderAndCollapsesNames(t *testing.T) {
	groups := GroupVehicles([]Vehicle{
		{Name: "Camry", Type: 1}, {Name: "Prius", Type: 2}, {Name: "camry", Type: 3}, {Name: "Camry", Type: 4},
	})
	if len(groups) != 2 || groups[0].Name != "Camry" || len(groups[0].Types) != 3 || groups[1].Name != "Prius" {
		t.Fatalf("%+v", groups)
	}
}

func TestVehicleECUCounts(t *testing.T) {
	d := fixture(t)
	counts, err := d.VehicleECUCounts(context.Background(), "NA")
	if err != nil || counts[12704] != 1 {
		t.Fatalf("%v %v", counts, err)
	}
}
