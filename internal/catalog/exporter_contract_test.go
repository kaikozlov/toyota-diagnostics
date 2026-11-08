package catalog

import (
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// exporterProjection builds a representative sqlite-v2 file the way
// tools/gts bundle sqlite does: structured tables plus item_sources
// pointers and zlib payloads, and no runtime_documents table.
func exporterProjection(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta VALUES ('schema','toyota-diagnostics-sqlite-v2'),('source_schema','toyota-diagnostics-bundle-v4'),('release','test'),('default_region','NA'),('member_count','1');
CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);
CREATE TABLE vehicles (region TEXT,vehicle_type INTEGER,name TEXT,install_set_ids TEXT);
CREATE TABLE vehicle_ecus (region TEXT, vehicle_type INTEGER, install_set_id INTEGER, category_id INTEGER, route_selector INTEGER, connection_frame_id INTEGER, route_key TEXT, request_address INTEGER, address_extension INTEGER, transport_kind TEXT);
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
CREATE TABLE item_sources (item_id INTEGER PRIMARY KEY, member TEXT, pointer TEXT);
CREATE TABLE item_payloads (item_id INTEGER PRIMARY KEY, data BLOB);
CREATE TABLE catalog_sources (region TEXT, category_id INTEGER, member TEXT, metadata TEXT);
`)
	if err != nil {
		t.Fatal(err)
	}
	category := map[string]any{
		"category_id": 405, "catalog_member": "catalogs/NA/405.json", "name": "Power Steering",
		"short_name": "eps", "database": "EMPS_P5.ddb", "generation": 20, "support_mode": "p5-standard",
		"support_family": "p5",
	}
	vehicle := map[string]any{"vehicle_type": 12704, "name": "Camry HV", "install_set_ids": []any{float64(8119)}}
	did := map[string]any{"name": "Steering Angle", "decoder": "p5-linear-msb0-v1", "bit_start": 0, "bit_end": 15, "unit": "deg"}
	dtc := map[string]any{"code": "C1234", "description": "Steer"}
	ffq := map[string]any{"freeze_frame_number": 1, "name": "FFD snapshot"}
	dataList := map[string]any{"name": "Steering Angle", "did": "0x1037"}
	ffd := map[string]any{"DataID": "5285", "DataName": "Arbitration"}
	binding := map[string]any{"role": 1, "dll": "utility.dll", "semantic_kind": "reset"}
	if _, err = db.Exec(`INSERT INTO ecus VALUES('NA',405,'eps','Power Steering','EMPS_P5.ddb',20,'p5-standard','catalogs/NA/405.json','{"dids":1}')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO vehicles VALUES('NA',12704,'Camry HV','[8119]')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO vehicle_ecus VALUES('NA',12704,8119,405,18,0,'405:18',1938,0,'iso15765-phase-family')`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO catalog_sources VALUES('NA',405,'catalogs/NA/405.json',?)`, `{"schema":"toyota-diagnostics-catalog-v4","source_identity":{"member":"catalogs/NA/405.json"}}`); err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(`INSERT INTO catalog_sources VALUES('NA',0,'customize/NA/0.json','{"schema":"toyota-diagnostics-customize-v4"}')`); err != nil {
		t.Fatal(err)
	}
	add := func(id int, region string, categoryID int, kind, ident, name, member, pointer string, payload any) {
		t.Helper()
		if _, err := db.Exec(`INSERT INTO items(id,region,category_id,kind,identifier,name,detail) VALUES(?,?,?,?,?,?,?)`, id, region, categoryID, kind, ident, name, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO item_payloads VALUES (?,?)`, id, packedDocument(t, payload)); err != nil {
			t.Fatal(err)
		}
		if member != "" {
			if _, err := db.Exec(`INSERT INTO item_sources VALUES (?,?,?)`, id, member, pointer); err != nil {
				t.Fatal(err)
			}
		}
	}
	add(1, "NA", 405, "did", "0x1037", "Steering Angle", "catalogs/NA/405.json", "/dids/0x1037/0", did)
	add(2, "NA", 405, "dtc", "C1234", "Steer", "catalogs/NA/405.json", "/dtcs/452696/0", dtc)
	add(3, "NA", 405, "data-list", "0x1037", "Steering Angle", "catalogs/NA/405.json", "/data_list/rows/0", dataList)
	add(4, "NA", 405, "freeze-frame-query", "1", "FFD snapshot", "catalogs/NA/405.json", "/support_catalog/freeze_frame_queries/0", ffq)
	add(5, "NA", 405, "ecu", "405", "Power Steering", "", "", category)
	add(6, "NA", 0, "vehicle", "12704", "Camry HV", "", "", vehicle)
	add(7, "NA", 0, "utility-family", "0x01", "reset", "", "", binding)
	add(8, "NA", 498, "ffd-signal", "0x5285", "Arbitration", "repo://data/generated/gtsplus_2026/pcs_data_viewer_tss3_managed_semantics.json", "/operation_ffd/detail_rows/0", ffd)
}

func TestExporterProjectionReconstructsIndexAndDocuments(t *testing.T) {
	path := filepath.Join(t.TempDir(), "export.sqlite")
	exporterProjection(t, path)
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := context.Background()
	if ok, err := db.hasTable(ctx, "runtime_documents"); err != nil || ok {
		t.Fatalf("fixture must not invent runtime_documents: %v %v", ok, err)
	}
	index, err := db.Index(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if index["projection"] != schema || index["default_region"] != "NA" {
		t.Fatalf("index metadata: %#v", index)
	}
	regional := index["regions"].(map[string]any)["NA"].(map[string]any)
	cat := regional["selected_categories"].(map[string]any)["405"].(map[string]any)
	if cat["catalog_member"] != "catalogs/NA/405.json" || cat["support_family"] != "p5" {
		t.Fatalf("selected category reconstructed from ecu payload: %#v", cat)
	}
	vehicle := regional["vehicles"].(map[string]any)["12704"].(map[string]any)
	if vehicle["name"] != "Camry HV" {
		t.Fatalf("vehicle: %#v", vehicle)
	}
	route := regional["routes"].(map[string]any)["405:18"].(map[string]any)
	if asIndexInt(route["physical_request_address"]) != 1938 {
		t.Fatalf("route: %#v", route)
	}
	install := regional["install_sets"].(map[string]any)["8119"].([]any)[0].(map[string]any)
	if asIndexInt(install["category_id"]) != 405 {
		t.Fatalf("install set: %#v", install)
	}
	if regional["customize_member"] != "customize/NA/0.json" {
		t.Fatalf("customize_member: %#v", regional["customize_member"])
	}
	bindings := regional["utilities"].(map[string]any)["bindings"].([]any)
	if len(bindings) != 1 || bindings[0].(map[string]any)["dll"] != "utility.dll" {
		t.Fatalf("utility bindings: %#v", bindings)
	}
	if _, ok := regional["session_control"]; ok {
		t.Fatal("sqlite projection must not invent session_control")
	}

	doc, err := db.Document(ctx, "catalogs/NA/405.json")
	if err != nil {
		t.Fatal(err)
	}
	if doc["schema"] != "toyota-diagnostics-catalog-v4" {
		t.Fatalf("metadata: %#v", doc)
	}
	gotDID := doc["dids"].(map[string]any)["0x1037"].([]any)[0].(map[string]any)
	if gotDID["name"] != "Steering Angle" {
		t.Fatalf("did: %#v", gotDID)
	}
	if doc["dtcs"].(map[string]any)["452696"].([]any)[0].(map[string]any)["code"] != "C1234" {
		t.Fatalf("dtc: %#v", doc["dtcs"])
	}
	rows := doc["data_list"].(map[string]any)["rows"].([]any)
	if rows[0].(map[string]any)["did"] != "0x1037" {
		t.Fatalf("data list: %#v", rows)
	}
	ff := doc["support_catalog"].(map[string]any)["freeze_frame_queries"].([]any)
	if asIndexInt(ff[0].(map[string]any)["freeze_frame_number"]) != 1 {
		t.Fatalf("freeze frame: %#v", ff)
	}

	supplement, err := db.Document(ctx, "supplement://pcs_data_viewer_tss3_managed_semantics.json")
	if err != nil {
		t.Fatal(err)
	}
	detail := supplement["operation_ffd"].(map[string]any)["detail_rows"].([]any)
	if detail[0].(map[string]any)["DataID"] != "5285" {
		t.Fatalf("supplement via alias: %#v", supplement)
	}

	again, err := db.Document(ctx, "catalogs/NA/405.json")
	if err != nil {
		t.Fatal(err)
	}
	doc["schema"] = "mutated"
	if again["schema"] == "mutated" {
		t.Fatal("reconstructed documents must not share mutable state")
	}
}

func TestExistingSearchFixtureReconstructsRegistryIndex(t *testing.T) {
	db := fixture(t)
	index, err := db.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	regional := index["regions"].(map[string]any)["NA"].(map[string]any)
	frc := regional["selected_categories"].(map[string]any)["498"].(map[string]any)
	if frc["catalog_member"] != "catalogs/NA/498.json" {
		t.Fatalf("%#v", frc)
	}
	if regional["vehicles"].(map[string]any)["12704"].(map[string]any)["name"] != "Camry HV" {
		t.Fatalf("%#v", regional["vehicles"])
	}
	route := regional["routes"].(map[string]any)["498:18"].(map[string]any)
	if asIndexInt(route["physical_request_address"]) != 1938 {
		t.Fatalf("%#v", route)
	}
}

func TestGzipDirectoryBundleSidecar(t *testing.T) {
	root := t.TempDir()
	writeGzipJSON(t, filepath.Join(root, "index.json.gz"), map[string]any{
		"schema":            "toyota-diagnostics-bundle-v4",
		"default_region":    "NA",
		"fault_status_mask": 175,
		"support_contracts": map[string]any{"p5": map[string]any{"kind": "p5"}},
		"regions": map[string]any{
			"NA": map[string]any{
				"selected_categories": map[string]any{"405": map[string]any{"catalog_member": "catalogs/NA/405.json"}},
				"session_control":     map[string]any{"kind": "uds", "per_category": map[string]any{"405": map[string]any{"extended_session": map[string]any{"comm_set": 1}}}},
			},
		},
	})
	writeGzipJSON(t, filepath.Join(root, "catalogs", "NA", "405.json.gz"), map[string]any{
		"dids": map[string]any{"0x1000": []any{map[string]any{"name": "FromBundle"}}},
	})
	path := filepath.Join(t.TempDir(), "browse.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = raw.Exec(`CREATE TABLE meta (key TEXT, value TEXT); INSERT INTO meta VALUES('schema','toyota-diagnostics-sqlite-v2'); CREATE TABLE ecus (region TEXT,category_id INTEGER,key TEXT,name TEXT,database_name TEXT,generation INTEGER,support_mode TEXT,catalog_member TEXT,counts TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AttachBundle(root); err != nil {
		t.Fatal(err)
	}
	index, err := db.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if asIndexInt(index["fault_status_mask"]) != 175 {
		t.Fatalf("sidecar index: %#v", index)
	}
	regional := index["regions"].(map[string]any)["NA"].(map[string]any)
	if regional["session_control"] == nil {
		t.Fatal("gzip-directory sidecar should supply session_control")
	}
	doc, err := db.Document(context.Background(), "catalogs/NA/405.json")
	if err != nil {
		t.Fatal(err)
	}
	if doc["dids"].(map[string]any)["0x1000"].([]any)[0].(map[string]any)["name"] != "FromBundle" {
		t.Fatalf("%#v", doc)
	}
}

func writeGzipJSON(t *testing.T, path string, value any) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err = w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err = w.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPointerCreateMatchesExporterLayout(t *testing.T) {
	doc := map[string]any{}
	if err := applyDocumentPointer(doc, "/dids/0x1000/0", map[string]any{"name": "A"}, true); err != nil {
		t.Fatal(err)
	}
	if err := applyDocumentPointer(doc, "/dids/0x1000/1", map[string]any{"name": "B"}, true); err != nil {
		t.Fatal(err)
	}
	if err := applyDocumentPointer(doc, "/legacy_rob/families/0/signals/0", map[string]any{"name": "S"}, true); err != nil {
		t.Fatal(err)
	}
	if err := applyDocumentPointer(doc, "/dtcs/452696/0", map[string]any{"code": "C1234"}, true); err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"dids":       map[string]any{"0x1000": []any{map[string]any{"name": "A"}, map[string]any{"name": "B"}}},
		"legacy_rob": map[string]any{"families": []any{map[string]any{"signals": []any{map[string]any{"name": "S"}}}}},
		"dtcs":       map[string]any{"452696": []any{map[string]any{"code": "C1234"}}},
	}
	if !reflect.DeepEqual(doc, want) {
		t.Fatalf("got %#v", doc)
	}
}
