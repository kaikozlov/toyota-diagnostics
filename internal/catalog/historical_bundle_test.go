package catalog

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"testing"
)

const (
	historicalBundleBlob    = "9ee6414c78f1a1632653c581c97d3ee926fa82b4"
	historicalBundleSHA256  = "47ef70ee8c2200fede7ac795cfccd09f624e95025ab2d6c131fe6710eaf4095d"
	historicalPCSBlob       = "d76554dca3e06f7f745a1ba6cff3d21e202cf142"
	historicalPCSSHA256     = "0d594c8e61ecb406aaf2a33320449b5aa24962877b71d95a0cd2018034cc4905"
	historicalScratchBundle = "/workspace/catalog-contract-scratch/toyota_current_diag.zip"
)

func findGitRoot() string {
	wd, err := os.Getwd()
	if err != nil {
		return ""
	}
	dir := wd
	for {
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func verifiedHistoricalBytes(t *testing.T, gitSHA, wantSHA256, envKey string, optionalPaths ...string) []byte {
	t.Helper()
	if path := os.Getenv(envKey); path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s=%s: %v", envKey, path, err)
		}
		if sha256Hex(data) != wantSHA256 {
			t.Fatalf("%s failed integrity check", envKey)
		}
		return data
	}
	if root := findGitRoot(); root != "" {
		data, err := exec.Command("git", "-C", root, "cat-file", "blob", gitSHA).Output()
		if err == nil {
			if sha256Hex(data) != wantSHA256 {
				t.Fatalf("git blob %s failed integrity check", gitSHA)
			}
			return data
		}
	}
	for _, path := range optionalPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		if sha256Hex(data) == wantSHA256 {
			return data
		}
	}
	t.Skipf("historical source %s is unavailable; set %s to a file matching sha256 %s", gitSHA, envKey, wantSHA256)
	return nil
}

func gitBlob(t *testing.T, sha string) []byte {
	t.Helper()
	if sha == historicalPCSBlob {
		return verifiedHistoricalBytes(t, historicalPCSBlob, historicalPCSSHA256, "TOYO_TEST_HISTORICAL_PCS")
	}
	return verifiedHistoricalBytes(t, sha, historicalBundleSHA256, "TOYO_TEST_HISTORICAL_BUNDLE", historicalScratchBundle)
}

func historicalBundlePath(t *testing.T) string {
	t.Helper()
	data := verifiedHistoricalBytes(t, historicalBundleBlob, historicalBundleSHA256, "TOYO_TEST_HISTORICAL_BUNDLE", historicalScratchBundle)
	path := filepath.Join(t.TempDir(), "toyota_current_diag.zip")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestHistoricalZipIsJSONMembersNotGzipTree(t *testing.T) {
	zr, err := zip.OpenReader(historicalBundlePath(t))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var index *zip.File
	for i := range zr.File {
		if zr.File[i].Name == "index.json" {
			index = zr.File[i]
			break
		}
	}
	if index == nil {
		t.Fatal("historical DEFAULT_REGISTRY zip has no index.json member")
	}
	rc, err := index.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()
	head := make([]byte, 1)
	if _, err := rc.Read(head); err != nil {
		t.Fatal(err)
	}
	if head[0] != '{' {
		t.Fatalf("index.json member should be raw JSON, got %q", head)
	}
}

func TestHistoricalBundleSidecarIndexAndSessionLifecycle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`CREATE TABLE meta (key TEXT, value TEXT); INSERT INTO meta VALUES('schema','toyota-diagnostics-sqlite-v2');`); err != nil {
		t.Fatal(err)
	}
	raw.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := db.AttachBundle(historicalBundlePath(t)); err != nil {
		t.Fatal(err)
	}
	index, err := db.Index(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if index["schema"] != "toyota-diagnostics-bundle-v2" {
		t.Fatalf("historical schema %v (this artifact predates bundle-v4)", index["schema"])
	}
	if index["default_region"] != "NA" {
		t.Fatalf("default_region %v", index["default_region"])
	}
	regional := index["regions"].(map[string]any)["NA"].(map[string]any)
	if regional["selected_categories"] == nil {
		t.Fatal("v2 categories should normalize to selected_categories")
	}
	frc := regional["selected_categories"].(map[string]any)["498"].(map[string]any)
	if frc["catalog_member"] != "catalogs/NA/498.json" {
		t.Fatalf("FRC category: %#v", frc)
	}
	session := regional["session_control"].(map[string]any)
	per := session["per_category"].(map[string]any)["498"].(map[string]any)
	if per["extended_session"] == nil || per["default_session"] == nil {
		t.Fatalf("historical session_control for 498: %#v", per)
	}
	doc, err := db.Document(context.Background(), "catalogs/NA/405.json")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := doc["dids"].(map[string]any)["0x1037"]; !ok {
		if _, ok = doc["dids"].(map[string]any)["0x0102"]; !ok {
			t.Fatalf("historical catalog member dids missing: %v", keysOf(doc["dids"]))
		}
	}
}

func TestHistoricalCatalogReconstructionFromExporterPointers(t *testing.T) {
	zr, err := zip.OpenReader(historicalBundlePath(t))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()
	var src map[string]any
	for _, file := range zr.File {
		if file.Name != "catalogs/NA/405.json" {
			continue
		}
		rc, err := file.Open()
		if err != nil {
			t.Fatal(err)
		}
		err = json.NewDecoder(rc).Decode(&src)
		rc.Close()
		if err != nil {
			t.Fatal(err)
		}
		break
	}
	if src == nil {
		t.Fatal("catalogs/NA/405.json missing from historical zip")
	}
	path := filepath.Join(t.TempDir(), "hist405.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta VALUES ('schema','toyota-diagnostics-sqlite-v2'),('source_schema','toyota-diagnostics-bundle-v2'),('release','hist'),('default_region','NA'),('member_count','1');
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
CREATE TABLE item_sources (item_id INTEGER PRIMARY KEY, member TEXT, pointer TEXT);
CREATE TABLE item_payloads (item_id INTEGER PRIMARY KEY, data BLOB);
CREATE TABLE catalog_sources (region TEXT, category_id INTEGER, member TEXT, metadata TEXT);
INSERT INTO catalog_sources VALUES('NA',405,'catalogs/NA/405.json','{"source_identity":{"member":"catalogs/NA/405.json"}}');
`); err != nil {
		t.Fatal(err)
	}
	id := 1
	insert := func(kind, ident, pointer string, value any) {
		t.Helper()
		if _, err := raw.Exec(`INSERT INTO items(id,region,category_id,kind,identifier,name,detail) VALUES(?,?,?,?,?,?,?)`, id, "NA", 405, kind, ident, ident, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`INSERT INTO item_payloads VALUES(?,?)`, id, packedDocument(t, value)); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`INSERT INTO item_sources VALUES(?,?,?)`, id, "catalogs/NA/405.json", pointer); err != nil {
			t.Fatal(err)
		}
		id++
	}
	for did, rows := range mapAny(src["dids"]) {
		for i, row := range anySlice(rows) {
			insert("did", did, "/dids/"+did+"/"+strconv.Itoa(i), row)
		}
	}
	for packed, rows := range mapAny(src["dtcs"]) {
		for i, row := range anySlice(rows) {
			insert("dtc", packed, "/dtcs/"+packed+"/"+strconv.Itoa(i), row)
		}
	}
	for i, row := range anySlice(mapAny(src["data_list"])["rows"]) {
		insert("data-list", strconv.Itoa(i), "/data_list/rows/"+strconv.Itoa(i), row)
	}
	raw.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Document(context.Background(), "catalogs/NA/405.json")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got["dids"], src["dids"]) {
		t.Fatal("reconstructed dids differ from historical catalogs/NA/405.json (exporter pointer contract)")
	}
	if !reflect.DeepEqual(got["dtcs"], src["dtcs"]) {
		t.Fatal("reconstructed dtcs differ from historical catalogs/NA/405.json")
	}
	if !reflect.DeepEqual(mapAny(got["data_list"])["rows"], mapAny(src["data_list"])["rows"]) {
		t.Fatal("reconstructed data_list.rows differ from historical source")
	}
}

func TestHistoricalPCSSupplementAliasReconstruction(t *testing.T) {
	var src map[string]any
	if err := json.Unmarshal(gitBlob(t, historicalPCSBlob), &src); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "pcs.sqlite")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = raw.Exec(`
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL);
INSERT INTO meta VALUES ('schema','toyota-diagnostics-sqlite-v2'),('source_schema','toyota-diagnostics-bundle-v4'),('release','hist'),('default_region','NA'),('member_count','0');
CREATE TABLE items (id INTEGER PRIMARY KEY,region TEXT,category_id INTEGER,kind TEXT,identifier TEXT,name TEXT,detail TEXT);
CREATE TABLE item_sources (item_id INTEGER PRIMARY KEY, member TEXT, pointer TEXT);
CREATE TABLE item_payloads (item_id INTEGER PRIMARY KEY, data BLOB);
`); err != nil {
		t.Fatal(err)
	}
	member := "repo://data/generated/gtsplus_2026/pcs_data_viewer_tss3_managed_semantics.json"
	id := 1
	for i, row := range anySlice(mapAny(src["operation_ffd"])["detail_rows"]) {
		m, _ := row.(map[string]any)
		dataID := ""
		if m != nil {
			dataID = stringify(m["DataID"])
		}
		if dataID == "" || dataID == "-" {
			continue
		}
		if _, err := raw.Exec(`INSERT INTO items(id,region,category_id,kind,identifier,name,detail) VALUES(?,?,?,?,?,?,?)`, id, "NA", 498, "ffd-signal", "0x"+dataID, dataID, ""); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`INSERT INTO item_payloads VALUES(?,?)`, id, packedDocument(t, row)); err != nil {
			t.Fatal(err)
		}
		if _, err := raw.Exec(`INSERT INTO item_sources VALUES(?,?,?)`, id, member, "/operation_ffd/detail_rows/"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
		id++
	}
	raw.Close()
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	got, err := db.Document(context.Background(), "supplement://pcs_data_viewer_tss3_managed_semantics.json")
	if err != nil {
		t.Fatal(err)
	}
	wantRows := anySlice(mapAny(src["operation_ffd"])["detail_rows"])
	gotRows := anySlice(mapAny(got["operation_ffd"])["detail_rows"])
	compared := 0
	for i, row := range wantRows {
		m, _ := row.(map[string]any)
		dataID := stringify(m["DataID"])
		if dataID == "" || dataID == "-" {
			continue
		}
		if i >= len(gotRows) || !reflect.DeepEqual(gotRows[i], row) {
			t.Fatalf("FFD row %d not restored from exporter-style supplement items", i)
		}
		compared++
	}
	if compared == 0 {
		t.Fatal("no PCS operation_ffd rows compared")
	}
}

func mapAny(v any) map[string]any {
	m, _ := v.(map[string]any)
	if m == nil {
		return map[string]any{}
	}
	return m
}

func anySlice(v any) []any {
	a, _ := v.([]any)
	return a
}

func stringify(v any) string {
	if v == nil {
		return ""
	}
	return stringifyValue(v)
}

func stringifyValue(v any) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return strconv.Itoa(asIndexInt(v))
	}
}

func keysOf(v any) []string {
	var keys []string
	for k := range mapAny(v) {
		keys = append(keys, k)
	}
	return keys
}
