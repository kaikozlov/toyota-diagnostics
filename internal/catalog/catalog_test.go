package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
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
INSERT INTO meta VALUES ('schema','toyota-diagnostics-sqlite-v1'),('source_schema','toyota-diagnostics-bundle-v4'),('release','test'),('default_region','NA'),('member_count','2');
CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);
INSERT INTO ecus VALUES('NA',498,'frc','Front Recognition Camera 2','FRC_P5.ddb',20,'p5-standard','catalogs/NA/498.json','{"dids":1,"signals":2}');
INSERT INTO ecus VALUES('NA',405,'eps','Power Steering','EMPS_P5.ddb',20,'p5-standard',null,'{}');
INSERT INTO ecus VALUES('NA',317,'eps_317','EPS','EPS_P4.ddb',4,'p4',null,'{}');
CREATE TABLE vehicles (region TEXT,vehicle_type INTEGER,name TEXT,install_set_ids TEXT);
CREATE TABLE vehicle_ecus (region TEXT, vehicle_type INTEGER, install_set_id INTEGER, category_id INTEGER, route_selector INTEGER, connection_frame_id INTEGER, route_key TEXT, request_address INTEGER, address_extension INTEGER, transport_kind TEXT);
INSERT INTO vehicle_ecus VALUES('NA',12704,8119,498,18,0,'498:18',1938,0,'iso15765-phase-family');
INSERT INTO vehicles VALUES('NA',12704,'Camry HV','[8119,8120]');
INSERT INTO vehicles VALUES('NA',12999,'Camry','[3]');
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
INSERT INTO items VALUES(1,'NA',498,'did','0x1601','LTA Control Condition','');
INSERT INTO items VALUES(2,'NA',498,'did','0x1601','LTA Switch Condition Flag','');
INSERT INTO items VALUES(3,'NA',498,'ffd-signal','0x5285','Arbitration result_lateral ID','TSS3 Operation FFD');
INSERT INTO items VALUES(4,'NA',0,'adu-signal','0x1593','Arbitration result Lateral ID','ADAS recorder');
CREATE VIRTUAL TABLE items_fts USING fts5(name,detail,identifier,content='items',content_rowid='id',tokenize='unicode61',prefix='2 3 4');
INSERT INTO items_fts(items_fts) VALUES('rebuild');
`
	if _, err := db.Exec(schema); err != nil {
		t.Fatal(err)
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
