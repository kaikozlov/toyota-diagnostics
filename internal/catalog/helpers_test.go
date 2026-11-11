package catalog

import (
	"bytes"
	"compress/zlib"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog/catalogtest"
)

// emptyDB is a writable database for record-level tests that create only the
// tables they exercise.
func emptyDB(t *testing.T) *DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "records.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return &DB{db: db}
}

// openFixture opens a finished catalogtest catalog the way the CLI does.
func openFixture(t *testing.T, c *catalogtest.Catalog) *DB {
	t.Helper()
	d, err := Open(c.Path())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// packedDocument stores JSON as the shipped catalog stores documents (zstd).
func packedDocument(t *testing.T, value any) []byte {
	t.Helper()
	return catalogtest.Pack(t, value)
}

// packedPart stores JSON as the shipped catalog stores document parts (zlib).
func packedPart(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return zlibPacked(t, string(raw))
}

func zlibPacked(t *testing.T, raw string) []byte {
	t.Helper()
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	if _, err := w.Write([]byte(raw)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}
