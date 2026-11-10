package catalog

import (
	"context"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func TestVerifyFindsCorruption(t *testing.T) {
	base := fixtureCatalog(t).Path()
	for name, corrupt := range map[string]func(t *testing.T, db *sql.DB, path string){
		"table page": func(t *testing.T, db *sql.DB, path string) {
			var root, size int64
			if err := db.QueryRow(`SELECT rootpage, (SELECT page_size FROM pragma_page_size) FROM sqlite_schema WHERE name='item_occurrences'`).Scan(&root, &size); err != nil {
				t.Fatal(err)
			}
			db.Close()
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			if _, err := f.WriteAt([]byte{0x01}, (root-1)*size); err != nil {
				t.Fatal(err)
			}
		},
		"schema page": func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			f, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			defer f.Close()
			// Page 1's fragmented-byte count, which only a check of the schema b-tree reads.
			if _, err := f.WriteAt([]byte{3}, 107); err != nil {
				t.Fatal(err)
			}
		},
		"unused page": func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			binary.BigEndian.PutUint32(raw[28:32], binary.BigEndian.Uint32(raw[28:32])+1)
			raw = append(raw, make([]byte, binary.BigEndian.Uint16(raw[16:18]))...)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"freelist page": func(t *testing.T, db *sql.DB, path string) {
			var root, size int64
			if err := db.QueryRow(`SELECT rootpage, (SELECT page_size FROM pragma_page_size) FROM sqlite_schema WHERE name='item_occurrences'`).Scan(&root, &size); err != nil {
				t.Fatal(err)
			}
			for _, q := range []string{"CREATE TABLE scratch(x)", "INSERT INTO scratch VALUES(zeroblob(100000))", "DROP TABLE scratch"} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			trunk := int64(binary.BigEndian.Uint32(raw[32:36]))
			if trunk == 0 || binary.BigEndian.Uint32(raw[(trunk-1)*size+4:]) == 0 {
				t.Fatal("no freelist leaves to corrupt")
			}
			binary.BigEndian.PutUint32(raw[(trunk-1)*size+8:], uint32(root))
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"freelist count": func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			binary.BigEndian.PutUint32(raw[36:40], binary.BigEndian.Uint32(raw[36:40])+1)
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
		},
		"full-text index": func(t *testing.T, db *sql.DB, _ string) {
			if _, err := db.Exec(`UPDATE items_fts_data SET block=zeroblob(length(block)) WHERE id=(SELECT max(id) FROM items_fts_data)`); err != nil {
				t.Fatal(err)
			}
			db.Close()
		},
	} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(base)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(t.TempDir(), "catalog.sqlite")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			corrupt(t, db, path)
			d, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer d.Close()
			if err := d.Verify(context.Background()); err == nil {
				t.Fatal("corruption passed verify")
			}
		})
	}
}
