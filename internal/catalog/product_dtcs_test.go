package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
)

func productDTCFixture(t *testing.T) *DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { conn.Close() })
	_, err = conn.Exec(`CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT UNIQUE);
 CREATE TABLE diagnostic_codes(id INTEGER PRIMARY KEY,code_id INTEGER,description_id INTEGER,comment_id INTEGER,failure_id INTEGER,identifier INTEGER,packed_code INTEGER,field_mask INTEGER);
 CREATE TABLE dtc_entries(id INTEGER PRIMARY KEY,code_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER,origin_mask INTEGER);
 INSERT INTO text_values VALUES(1,'P1234'),(2,'説明'),(3,'ECU.ddb'),(4,'');
 INSERT INTO diagnostic_codes VALUES(1,1,2,NULL,NULL,NULL,1192960,43),(2,1,NULL,4,NULL,9007199254740993,NULL,22),(3,1,NULL,NULL,NULL,NULL,NULL,0);
 INSERT INTO dtc_entries VALUES(1,1,3,65,7,7),(2,2,NULL,NULL,NULL,0),(3,3,NULL,NULL,NULL,7);`)
	if err != nil {
		t.Fatal(err)
	}
	return &DB{db: conn}
}
func TestProductDTCDecodedFacts(t *testing.T) {
	d := productDTCFixture(t)
	value, err := d.diagnosticCode(context.Background(), 1)
	if err != nil {
		t.Fatal(err)
	}
	r := value.(map[string]any)
	if r["packed_dtc"] != "0x123400" || r["description"] != "説明" || r["failure"] != nil || r["table"] != json.Number("65") {
		t.Fatal(r)
	}
	value, err = d.diagnosticCode(context.Background(), 2)
	if err != nil {
		t.Fatal(err)
	}
	r = value.(map[string]any)
	if r["dtc_identifier"] != json.Number("9007199254740993") || r["comment"] != "" {
		t.Fatal(r)
	}
	if _, ok := r["source"]; ok {
		t.Fatal("invented absent origin")
	}
	value, err = d.diagnosticCode(context.Background(), 3)
	if err != nil {
		t.Fatal(err)
	}
	r = value.(map[string]any)
	if v, ok := r["source"]; !ok || v != nil {
		t.Fatal("lost explicit null origin")
	}
}
func TestProductDTCRuntimeTypesAndIsolation(t *testing.T) {
	d := productDTCFixture(t)
	ctx := context.Background()
	value, err := d.runtimeValue(ctx, map[string]any{"$diagnostic_code": json.Number("1")})
	if err != nil {
		t.Fatal(err)
	}
	row := value.(map[string]any)
	if row["table"] != float64(65) || row["record"] != float64(7) {
		t.Fatalf("runtime numeric types: %#v", row)
	}
	row["description"] = "changed"
	again, err := d.diagnosticCode(ctx, 1)
	if err != nil || again.(map[string]any)["description"] != "説明" {
		t.Fatal("mutated cached definition", again, err)
	}
}
func TestProductDTCCorruptReference(t *testing.T) {
	d := productDTCFixture(t)
	ctx := context.Background()
	if _, err := d.diagnosticCode(ctx, 99); err == nil {
		t.Fatal("accepted missing entry")
	}
	if _, err := d.db.Exec("DELETE FROM text_values WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.diagnosticCode(ctx, 1); err == nil {
		t.Fatal("accepted missing description")
	}
	if _, err := d.db.Exec("UPDATE diagnostic_codes SET field_mask=128 WHERE id=2"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.diagnosticCode(ctx, 2); err == nil {
		t.Fatal("accepted invalid presence mask")
	}
}
