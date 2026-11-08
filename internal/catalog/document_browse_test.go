package catalog

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowseOnlyCatalogOpensAndExplainsMissingRuntime(t *testing.T) {
	path := filepath.Join(t.TempDir(), "browse.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE meta (key TEXT, value TEXT);
INSERT INTO meta VALUES('schema','toyota-diagnostics-sqlite-v2'),('release','test');
CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);
INSERT INTO ecus VALUES('NA',498,'frc','Front Recognition Camera 2','FRC_P5.ddb',20,'p5-standard',null,'{}');
CREATE TABLE vehicles (region TEXT,vehicle_type INTEGER,name TEXT,install_set_ids TEXT);
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ecus, err := db.ECUs(context.Background(), "NA", "", 10)
	if err != nil || len(ecus) != 1 || ecus[0].Key != "frc" {
		t.Fatalf("browse lookup failed: %v %+v", err, ecus)
	}
	_, err = db.Document(context.Background(), "index.json")
	var missing *MissingRuntimeMetadataError
	if !errors.As(err, &missing) || !strings.Contains(err.Error(), "browse-only") || strings.Contains(err.Error(), "no such table") {
		t.Fatalf("want actionable browse-only error, got %v", err)
	}
}

func TestRuntimeDocumentErrorsAreActionable(t *testing.T) {
	db, raw := documentFixture(t)
	defer raw.Close()
	defer db.Close()
	ctx := context.Background()
	_, err := db.Document(ctx, "index.json")
	if err == nil || !strings.Contains(err.Error(), "index.json") || strings.Contains(err.Error(), "no rows") {
		t.Fatalf("%v", err)
	}
	_, err = db.Document(ctx, "supplement://pcs_data_viewer_tss3_managed_semantics.json")
	if err == nil || !strings.Contains(err.Error(), "FFD") || !strings.Contains(err.Error(), "supplement://") {
		t.Fatalf("%v", err)
	}
}
