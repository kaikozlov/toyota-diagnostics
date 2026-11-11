package catalog

import (
	"context"
	"database/sql"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// verifyModes are the auto-vacuum settings a catalog file can carry.
var verifyModes = []string{"none", "full", "incremental"}

// verifyCopy writes base to a fresh file with the given auto-vacuum mode.
func verifyCopy(t *testing.T, base, mode string) string {
	t.Helper()
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
	defer db.Close()
	for _, q := range []string{"PRAGMA auto_vacuum=" + mode, "VACUUM"} {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

// quickCheck runs SQLite's whole-file quick_check.
func quickCheck(t *testing.T, path string) string {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?mode=ro")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return err.Error()
	}
	return result
}

func verifyFile(t *testing.T, path string) error {
	t.Helper()
	d, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	return d.Verify(context.Background())
}

func TestVerifyPassesEveryAutoVacuumMode(t *testing.T) {
	base := fixtureCatalog(t).Path()
	for _, mode := range verifyModes {
		t.Run(mode, func(t *testing.T) {
			path := verifyCopy(t, base, mode)
			db, err := sql.Open("sqlite", path)
			if err != nil {
				t.Fatal(err)
			}
			// Leave free pages behind where the mode keeps them.
			for _, q := range []string{"CREATE TABLE scratch(x)", "INSERT INTO scratch VALUES(zeroblob(100000))", "DROP TABLE scratch"} {
				if _, err := db.Exec(q); err != nil {
					t.Fatal(err)
				}
			}
			db.Close()
			if result := quickCheck(t, path); result != "ok" {
				t.Fatal(result)
			}
			if err := verifyFile(t, path); err != nil {
				t.Fatal(err)
			}
		})
	}
}

// file edits a closed catalog file in place.
func file(t *testing.T, path string, edit func(raw []byte, pageSize int64) []byte) {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(binary.BigEndian.Uint16(raw[16:18]))
	if err := os.WriteFile(path, edit(raw, size), 0600); err != nil {
		t.Fatal(err)
	}
}

// pointerMapEntry returns the file offset of page's pointer-map entry.
func pointerMapEntry(raw []byte, pageSize, page int64) int64 {
	perMap := (pageSize-int64(raw[20]))/5 + 1
	m := (page-2)/perMap*perMap + 2
	return (m-1)*pageSize + 5*(page-m-1)
}

func scalar(t *testing.T, db *sql.DB, query string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func exec(t *testing.T, db *sql.DB, queries ...string) {
	t.Helper()
	for _, q := range queries {
		if _, err := db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestVerifyFindsCorruption(t *testing.T) {
	base := fixtureCatalog(t).Path()
	// Each case corrupts an open copy, closing db before it edits the file.
	// skip names a mode the corruption cannot happen in.
	type corruption struct {
		skip    func(mode string) bool
		corrupt func(t *testing.T, db *sql.DB, path string)
	}
	autoVacuumOnly := func(mode string) bool { return mode == "none" }
	keepsFreePages := func(mode string) bool { return mode == "full" }
	freePages := func(t *testing.T, db *sql.DB) {
		exec(t, db, "CREATE TABLE scratch(x)", "INSERT INTO scratch VALUES(zeroblob(100000))", "DROP TABLE scratch")
	}
	// corruptEntry rewrites the pointer-map entry of the page query names.
	corruptEntry := func(query string, entry []byte) func(t *testing.T, db *sql.DB, path string) {
		return func(t *testing.T, db *sql.DB, path string) {
			exec(t, db, "CREATE TABLE wide(x)", "WITH RECURSIVE n(i) AS (SELECT 1 UNION ALL SELECT i+1 FROM n WHERE i<400) INSERT INTO wide SELECT randomblob(300) FROM n",
				"INSERT INTO wide VALUES(zeroblob(20000))")
			page := scalar(t, db, query)
			db.Close()
			file(t, path, func(raw []byte, size int64) []byte {
				copy(raw[pointerMapEntry(raw, size, page):], entry)
				return raw
			})
		}
	}
	cases := map[string]corruption{
		"table page": {corrupt: func(t *testing.T, db *sql.DB, path string) {
			root := scalar(t, db, `SELECT rootpage FROM sqlite_schema WHERE name='item_occurrences'`)
			db.Close()
			file(t, path, func(raw []byte, size int64) []byte {
				raw[(root-1)*size] = 0x01
				return raw
			})
		}},
		"schema page": {corrupt: func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			// Page 1's fragmented-byte count, which only a check of the schema b-tree reads.
			file(t, path, func(raw []byte, _ int64) []byte {
				raw[107] = 3
				return raw
			})
		}},
		"unused page": {corrupt: func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			file(t, path, func(raw []byte, size int64) []byte {
				binary.BigEndian.PutUint32(raw[28:32], binary.BigEndian.Uint32(raw[28:32])+1)
				return append(raw, make([]byte, size)...)
			})
		}},
		"freelist page": {skip: keepsFreePages, corrupt: func(t *testing.T, db *sql.DB, path string) {
			root := scalar(t, db, `SELECT rootpage FROM sqlite_schema WHERE name='item_occurrences'`)
			freePages(t, db)
			db.Close()
			file(t, path, func(raw []byte, size int64) []byte {
				trunk := int64(binary.BigEndian.Uint32(raw[32:36]))
				if trunk == 0 || binary.BigEndian.Uint32(raw[(trunk-1)*size+4:]) == 0 {
					t.Fatal("no freelist leaves to corrupt")
				}
				binary.BigEndian.PutUint32(raw[(trunk-1)*size+8:], uint32(root))
				return raw
			})
		}},
		"freelist count": {corrupt: func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			file(t, path, func(raw []byte, _ int64) []byte {
				binary.BigEndian.PutUint32(raw[36:40], binary.BigEndian.Uint32(raw[36:40])+1)
				return raw
			})
		}},
		"full-text index": {corrupt: func(t *testing.T, db *sql.DB, _ string) {
			exec(t, db, `UPDATE items_fts_data SET block=zeroblob(length(block)) WHERE id=(SELECT max(id) FROM items_fts_data)`)
			db.Close()
		}},
		"incremental vacuum without auto-vacuum": {skip: func(mode string) bool { return mode != "none" }, corrupt: func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			file(t, path, func(raw []byte, _ int64) []byte {
				binary.BigEndian.PutUint32(raw[64:68], 1)
				return raw
			})
		}},
		"largest root page": {skip: autoVacuumOnly, corrupt: func(t *testing.T, db *sql.DB, path string) {
			db.Close()
			file(t, path, func(raw []byte, _ int64) []byte {
				binary.BigEndian.PutUint32(raw[52:56], binary.BigEndian.Uint32(raw[52:56])+1)
				return raw
			})
		}},
		"unreadable root entry":     {skip: autoVacuumOnly, corrupt: corruptEntry(`SELECT rootpage FROM sqlite_schema WHERE name='wide'`, []byte{0})},
		"wrong root entry":          {skip: autoVacuumOnly, corrupt: corruptEntry(`SELECT rootpage FROM sqlite_schema WHERE name='wide'`, []byte{5})},
		"wrong root parent":         {skip: autoVacuumOnly, corrupt: corruptEntry(`SELECT rootpage FROM sqlite_schema WHERE name='wide'`, []byte{1, 0, 0, 0, 2})},
		"wrong b-tree page entry":   {skip: autoVacuumOnly, corrupt: corruptEntry(`SELECT pageno FROM dbstat WHERE name='wide' AND path!='/' AND pagetype!='overflow' LIMIT 1`, []byte{5, 0, 0, 0, 1})},
		"wrong overflow entry":      {skip: autoVacuumOnly, corrupt: corruptEntry(`SELECT pageno FROM dbstat WHERE name='wide' AND pagetype='overflow' LIMIT 1 OFFSET 1`, []byte{3})},
		"unreadable overflow entry": {skip: autoVacuumOnly, corrupt: corruptEntry(`SELECT pageno FROM dbstat WHERE name='wide' AND pagetype='overflow' LIMIT 1`, []byte{9})},
		"wrong free page entry": {skip: func(mode string) bool { return mode != "incremental" }, corrupt: func(t *testing.T, db *sql.DB, path string) {
			freePages(t, db)
			db.Close()
			file(t, path, func(raw []byte, size int64) []byte {
				trunk := int64(binary.BigEndian.Uint32(raw[32:36]))
				if trunk == 0 {
					t.Fatal("no free pages")
				}
				raw[pointerMapEntry(raw, size, trunk)] = 5
				return raw
			})
		}},
	}
	for _, mode := range verifyModes {
		for name, c := range cases {
			if c.skip != nil && c.skip(mode) {
				continue
			}
			t.Run(mode+"/"+name, func(t *testing.T) {
				path := verifyCopy(t, base, mode)
				db, err := sql.Open("sqlite", path)
				if err != nil {
					t.Fatal(err)
				}
				c.corrupt(t, db, path)
				if quickCheck(t, path) == "ok" {
					t.Fatal("quick_check passes the corrupt file")
				}
				if err := verifyFile(t, path); err == nil {
					t.Fatal("corruption passed verify")
				}
			})
		}
	}
}
