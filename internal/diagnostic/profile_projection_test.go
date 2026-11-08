package diagnostic

import (
	"bytes"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
	_ "modernc.org/sqlite"
)

func TestNewProfileReadsExporterProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta VALUES ('schema','toyota-diagnostics-sqlite-v2'),('source_schema','toyota-diagnostics-bundle-v4'),('release','test'),('default_region','NA'),('member_count','1');
CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);
INSERT INTO ecus VALUES('NA',405,'eps','Power Steering','EMPS_P5.ddb',20,'p5-standard','catalogs/NA/405.json','{}');
CREATE TABLE vehicles (region TEXT,vehicle_type INTEGER,name TEXT,install_set_ids TEXT);
INSERT INTO vehicles VALUES('NA',12704,'Camry HV','[8119]');
CREATE TABLE vehicle_ecus (region TEXT, vehicle_type INTEGER, install_set_id INTEGER, category_id INTEGER, route_selector INTEGER, connection_frame_id INTEGER, route_key TEXT, request_address INTEGER, address_extension INTEGER, transport_kind TEXT);
INSERT INTO vehicle_ecus VALUES('NA',12704,8119,405,18,0,'405:18',1938,0,'iso15765-phase-family');
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
CREATE TABLE item_payloads (item_id INTEGER PRIMARY KEY, data BLOB);
`)
	if err != nil {
		t.Fatal(err)
	}
	payloads := map[int]any{
		1: map[string]any{"category_id": 405, "catalog_member": "catalogs/NA/405.json", "name": "Power Steering", "short_name": "eps", "generation": 20, "support_family": "p5"},
		2: map[string]any{"vehicle_type": 12704, "name": "Camry HV", "install_set_ids": []any{float64(8119)}},
	}
	if _, err = raw.Exec(`INSERT INTO items(id,region,category_id,kind,identifier,name,detail) VALUES(1,'NA',405,'ecu','405','Power Steering',''),(2,'NA',0,'vehicle','12704','Camry HV','')`); err != nil {
		t.Fatal(err)
	}
	for id, payload := range payloads {
		b, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		var buf bytes.Buffer
		w := zlib.NewWriter(&buf)
		if _, err = w.Write(b); err != nil {
			t.Fatal(err)
		}
		if err = w.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err = raw.Exec(`INSERT INTO item_payloads VALUES(?,?)`, id, buf.Bytes()); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()
	db, err := catalog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	p, err := NewProfile(context.Background(), db, "NA", "12704", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.ECUs) != 1 || p.ECUs[0].Key != "eps" || p.ECUs[0].Address != 1938 || !p.ECUs[0].RouteResolved {
		t.Fatalf("registry lookup from exporter tables: %+v", p.ECUs)
	}
	if p.Vehicle["name"] != "Camry HV" {
		t.Fatalf("vehicle: %#v", p.Vehicle)
	}
}

func TestNewProfileHistoricalBundleSessionLifecycle(t *testing.T) {
	bundle := "/workspace/catalog-contract-scratch/toyota_current_diag.zip"
	if _, err := os.Stat(bundle); err != nil {
		cmd := exec.Command("git", "rev-parse", "--show-toplevel")
		cmd.Dir = "."
		root, err := cmd.Output()
		if err != nil {
			t.Skip(err)
		}
		data, err := exec.Command("git", "-C", strings.TrimSpace(string(root)), "cat-file", "blob", "9ee6414c78f1a1632653c581c97d3ee926fa82b4").Output()
		if err != nil {
			t.Skipf("historical bundle blob unavailable: %v", err)
		}
		bundle = filepath.Join(t.TempDir(), "toyota_current_diag.zip")
		if err := os.WriteFile(bundle, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(t.TempDir(), "empty.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`CREATE TABLE meta (key TEXT, value TEXT); INSERT INTO meta VALUES('schema','toyota-diagnostics-sqlite-v2'); CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := catalog.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AttachBundle(bundle); err != nil {
		t.Fatal(err)
	}
	p, err := NewProfile(context.Background(), db, "NA", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	var frc *EcuSpec
	for i := range p.ECUs {
		if p.ECUs[i].CategoryID == 498 {
			frc = &p.ECUs[i]
			break
		}
	}
	if frc == nil {
		t.Fatal("historical index missing category 498")
	}
	life, err := ParseLifecycle(p.SessionControl(), 498)
	if err != nil || life == nil || life.ExtendedSession == 0 && len(life.EnterSequence) == 0 {
		t.Fatalf("session lifecycle from historical sidecar: %v %#v", err, life)
	}
}
