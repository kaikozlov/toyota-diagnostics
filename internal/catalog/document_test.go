package catalog

import (
	"bytes"
	"compress/gzip"
	"compress/zlib"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// Run against a generated catalog with TOYOTA_TEST_CATALOG and TOYOTA_CANONICAL_BUNDLE.
func TestDocumentCanonicalParity(t *testing.T) {
	path, root := os.Getenv("TOYOTA_TEST_CATALOG"), os.Getenv("TOYOTA_CANONICAL_BUNDLE")
	if path == "" || root == "" {
		t.Skip("canonical bundle integration requires explicit paths")
	}
	db, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	files, err := filepath.Glob(filepath.Join(root, "catalogs", "*", "*.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	custom, err := filepath.Glob(filepath.Join(root, "customize", "*", "*.json.gz"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, custom...)
	files = append(files, filepath.Join(root, "index.json.gz"))
	if len(files) < 2 {
		t.Fatal("no canonical catalog documents found")
	}
	started := time.Now()
	for _, path := range files {
		member, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		member = strings.TrimSuffix(filepath.ToSlash(member), ".gz")
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		r, err := gzip.NewReader(file)
		if err != nil {
			file.Close()
			t.Fatal(err)
		}
		var want map[string]any
		err = json.NewDecoder(r).Decode(&want)
		r.Close()
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
		got, err := db.Document(context.Background(), member)
		if err != nil {
			t.Fatalf("%s: %v", member, err)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s: reconstructed document differs from canonical source", member)
		}
	}
	t.Logf("Exact equality for %d canonical documents in %s", len(files), time.Since(started))
}
func documentFixture(t *testing.T) (*DB, *sql.DB) {
	t.Helper()
	db, e := sql.Open("sqlite", filepath.Join(t.TempDir(), "documents.sqlite"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { db.Close() })
	_, e = db.Exec("CREATE TABLE runtime_documents(member TEXT PRIMARY KEY,data BLOB); CREATE TABLE item_sources(item_id INTEGER PRIMARY KEY,member TEXT,pointer TEXT); CREATE TABLE item_payloads(item_id INTEGER PRIMARY KEY,data BLOB)")
	if e != nil {
		t.Fatal(e)
	}
	return &DB{db: db}, db
}
func packedDocument(t *testing.T, value any) []byte {
	t.Helper()
	b, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	if _, e = w.Write(b); e != nil {
		t.Fatal(e)
	}
	if e = w.Close(); e != nil {
		t.Fatal(e)
	}
	return out.Bytes()
}
func TestDocumentRoundTrip(t *testing.T) {
	d, db := documentFixture(t)
	ctx := context.Background()
	skeleton := map[string]any{"metadata": map[string]any{"boundary": "exact", "empty": []any{}}, "dids": map[string]any{"0x1000": []any{nil, nil}}, "a/b": map[string]any{"~key": []any{nil}}, "nil": nil}
	member := "catalogs/NA/123.json"
	if _, e := db.Exec("INSERT INTO runtime_documents VALUES (?,?)", member, packedDocument(t, skeleton)); e != nil {
		t.Fatal(e)
	}
	records := []struct {
		pointer string
		value   any
	}{
		{"/dids/0x1000/0", map[string]any{"name": "Speed", "nested": []any{float64(1), true, nil}}},
		{"/dids/0x1000/1", map[string]any{"name": "Speed", "same_id": float64(1)}},
		{"/a~1b/~0key/0", map[string]any{"name": "Escaped"}},
		{"/dids/0x1000/0", map[string]any{"name": "Speed", "nested": []any{float64(1), true, nil}}},
	}
	for i, r := range records {
		if _, e := db.Exec("INSERT INTO item_sources VALUES (?,?,?)", i, member, r.pointer); e != nil {
			t.Fatal(e)
		}
		if _, e := db.Exec("INSERT INTO item_payloads VALUES (?,?)", i, packedDocument(t, r.value)); e != nil {
			t.Fatal(e)
		}
	}
	got, e := d.Document(ctx, member)
	if e != nil {
		t.Fatal(e)
	}
	skeleton["dids"].(map[string]any)["0x1000"] = []any{records[0].value, records[1].value}
	skeleton["a/b"].(map[string]any)["~key"] = []any{records[2].value}
	if !reflect.DeepEqual(got, skeleton) {
		t.Fatalf("got %#v, want %#v", got, skeleton)
	}
	got["metadata"] = nil
	again, e := d.Document(ctx, member)
	if e != nil || again["metadata"] == nil {
		t.Fatal("returned document shared mutable state", again, e)
	}
	if _, e := d.Document(ctx, "missing"); e == nil || !strings.Contains(e.Error(), "regenerate") {
		t.Fatal(e)
	}
}
func TestDocumentInvalidPayloads(t *testing.T) {
	for _, tc := range []struct {
		name    string
		data    []byte
		pointer string
		item    []byte
	}{
		{"bad compression", []byte("bad"), "", nil},
		{"null root", packedDocument(t, nil), "", nil},
		{"array root", packedDocument(t, []any{}), "", nil},
		{"bad item", packedDocument(t, map[string]any{"rows": []any{nil}}), "/rows/0", []byte("bad")},
		{"bad pointer", packedDocument(t, map[string]any{"rows": []any{nil}}), "/rows/1", packedDocument(t, map[string]any{"x": 1})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, db := documentFixture(t)
			if _, e := db.Exec("INSERT INTO runtime_documents VALUES ('x',?)", tc.data); e != nil {
				t.Fatal(e)
			}
			if tc.pointer != "" {
				db.Exec("INSERT INTO item_sources VALUES(1,'x',?)", tc.pointer)
				db.Exec("INSERT INTO item_payloads VALUES(1,?)", tc.item)
			}
			if _, e := d.Document(context.Background(), "x"); e == nil {
				t.Fatal("malformed document accepted")
			}
		})
	}
}
func TestDocumentMissingIndexedPayloadFails(t *testing.T) {
	d, db := documentFixture(t)
	if _, err := db.Exec("INSERT INTO runtime_documents VALUES ('x',?)", packedDocument(t, map[string]any{"rows": []any{nil}})); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO item_sources VALUES (1,'x','/rows/0')"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.Document(context.Background(), "x"); err == nil {
		t.Fatal("missing source payload silently became null")
	}
}
func TestDocumentPointerValidation(t *testing.T) {
	for _, p := range []string{"", "rows/0", "/missing", "/rows/2", "/rows/-1", "/rows/+0", "/rows/00", "/rows/not-a-number", "/rows/0/child", "/bad~2escape", "/~"} {
		doc := map[string]any{"rows": []any{nil}}
		if e := setDocumentPointer(doc, p, "value"); e == nil {
			t.Fatal("accepted", p)
		}
	}
	doc := map[string]any{"": nil, "~1": nil}
	if e := setDocumentPointer(doc, "/", "empty-key"); e != nil {
		t.Fatal(e)
	}
	if e := setDocumentPointer(doc, "/~01", "literal-tilde-one"); e != nil {
		t.Fatal(e)
	}
	if doc[""] != "empty-key" || doc["~1"] != "literal-tilde-one" {
		t.Fatal(doc)
	}
}
