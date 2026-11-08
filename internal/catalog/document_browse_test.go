package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

func TestBrowseOnlyCatalogOpensAndExplainsMissingDocuments(t *testing.T) {
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
	index, err := db.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	cat := index["regions"].(map[string]any)["NA"].(map[string]any)["selected_categories"].(map[string]any)["498"].(map[string]any)
	if cat["catalog_member"] != "" {
		t.Fatalf("browse-only ECU should not invent a catalog member: %#v", cat)
	}
	_, err = db.Document(context.Background(), "catalogs/NA/498.json")
	var missing *MissingRuntimeMetadataError
	if !errors.As(err, &missing) || strings.Contains(err.Error(), "no such table") {
		t.Fatalf("want actionable missing catalog document error, got %v", err)
	}
	_, err = db.Document(context.Background(), "supplement://pcs_data_viewer_tss3_managed_semantics.json")
	if !errors.As(err, &missing) || !strings.Contains(err.Error(), "FFD") || !strings.Contains(err.Error(), "supplement://") {
		t.Fatalf("want FFD supplement error, got %v", err)
	}
}

func TestRuntimeDocumentErrorsAreActionable(t *testing.T) {
	db, raw := documentFixture(t)
	defer raw.Close()
	defer db.Close()
	ctx := context.Background()
	_, err := db.Document(ctx, "index.json")
	if err == nil || !strings.Contains(err.Error(), "browse-only") || strings.Contains(err.Error(), "no rows") {
		t.Fatalf("%v", err)
	}
	_, err = db.Document(ctx, "supplement://pcs_data_viewer_tss3_managed_semantics.json")
	if err == nil || !strings.Contains(err.Error(), "FFD") || !strings.Contains(err.Error(), "supplement://") {
		t.Fatalf("%v", err)
	}
}

func TestDocumentDoesNotAliasRegionalCatalogMembers(t *testing.T) {
	d, db := documentFixture(t)
	ctx := context.Background()
	for _, region := range []string{"NA", "EU"} {
		member := fmt.Sprintf("catalogs/%s/405.json", region)
		if _, err := db.Exec("INSERT INTO runtime_documents VALUES (?,?)", member, packedDocument(t, map[string]any{"region": region})); err != nil {
			t.Fatal(err)
		}
	}
	got, err := d.Document(ctx, "catalogs/NA/405.json")
	if err != nil || got["region"] != "NA" {
		t.Fatalf("NA member: %v %#v", err, got)
	}
	got, err = d.Document(ctx, "catalogs/EU/405.json")
	if err != nil || got["region"] != "EU" {
		t.Fatalf("EU member: %v %#v", err, got)
	}
}

func TestAmbiguousSupplementURIDoesNotPickARepoCandidate(t *testing.T) {
	d, db := documentFixture(t)
	members := []string{
		"repo://data/generated/gtsplus_2026/pcs_data_viewer_tss3_managed_semantics.json",
		"repo://data/generated/other/pcs_data_viewer_tss3_managed_semantics.json",
	}
	for i, member := range members {
		if _, err := db.Exec("INSERT INTO item_sources VALUES (?,?,?)", i+1, member, "/operation_ffd/detail_rows/0"); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec("INSERT INTO item_payloads VALUES (?,?)", i+1, packedDocument(t, map[string]any{"src": member})); err != nil {
			t.Fatal(err)
		}
	}
	_, err := d.Document(context.Background(), "supplement://pcs_data_viewer_tss3_managed_semantics.json")
	if err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("want ambiguous URI error, got %v", err)
	}
}
