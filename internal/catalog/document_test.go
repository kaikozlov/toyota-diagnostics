package catalog

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

// Optional full-product fixtures contain expected diagnostic documents.
func TestDocumentsMatchFixtures(t *testing.T) {
	path, root := os.Getenv("TOYO_TEST_CATALOG"), os.Getenv("TOYO_TEST_DOCUMENTS")
	if path == "" || root == "" {
		t.Skip("product document integration requires explicit fixture paths")
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
		t.Fatal("no product catalog documents found")
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
			t.Fatalf("%s: diagnostic document differs from product fixture", member)
		}
	}
	t.Logf("Exact equality for %d product documents in %s", len(files), time.Since(started))
}
func TestDocumentRoundTrip(t *testing.T) {
	c := catalogtest.New(t)
	member := "catalogs/NA/123.json"
	stored := map[string]any{"metadata": map[string]any{"boundary": "exact", "empty": []any{}}, "dids": map[string]any{"0x1000": []any{map[string]any{"name": "Speed", "nested": []any{1, true, nil}}}}, "a/b": map[string]any{"~key": []any{"Escaped"}}, "nil": nil}
	c.Document(member, stored)
	d := openFixture(t, c)
	ctx := context.Background()
	got, e := d.Document(ctx, member)
	if e != nil {
		t.Fatal(e)
	}
	want := map[string]any{"metadata": map[string]any{"boundary": "exact", "empty": []any{}}, "dids": map[string]any{"0x1000": []any{map[string]any{"name": "Speed", "nested": []any{float64(1), true, nil}}}}, "a/b": map[string]any{"~key": []any{"Escaped"}}, "nil": nil}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	if v, e := d.sourceValue(ctx, member, "/a~1b/~0key/0"); e != nil || v != "Escaped" {
		t.Fatal(v, e)
	}
	got["metadata"] = nil
	again, e := d.Document(ctx, member)
	if e != nil || again["metadata"] == nil {
		t.Fatal("returned document shared mutable state", again, e)
	}
	if _, e := d.Document(ctx, "missing"); e == nil || !strings.Contains(e.Error(), `no document "missing"`) {
		t.Fatal(e)
	}
}
func TestDocumentInvalidPayloads(t *testing.T) {
	for _, tc := range []struct {
		name string
		data func(t *testing.T) []byte
	}{
		{"bad compression", func(*testing.T) []byte { return []byte("bad") }},
		{"null root", func(t *testing.T) []byte { return packedDocument(t, nil) }},
		{"array root", func(t *testing.T) []byte { return packedDocument(t, []any{}) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := catalogtest.New(t)
			c.Exec("INSERT INTO source_documents(member,data) VALUES('x',?)", tc.data(t))
			if _, e := openFixture(t, c).Document(context.Background(), "x"); e == nil {
				t.Fatal("malformed document accepted")
			}
		})
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
