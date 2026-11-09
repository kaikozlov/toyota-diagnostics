package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func configurationFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	_, err := d.db.Exec("CREATE TABLE ecu_catalog_definitions(id INTEGER PRIMARY KEY,data BLOB);CREATE TABLE ecu_catalog_bindings(document_id INTEGER PRIMARY KEY,definition_id INTEGER,category BLOB)")
	if err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"signals": []any{map[string]any{"name": "Useful", "value": json.Number("18446744073709551615")}}, "unrelated": map[string]any{"$signal_info": json.Number("99999")}}
	if _, err = d.db.Exec("INSERT INTO ecu_catalog_definitions VALUES(1,?)", packedDocument(t, body)); err != nil {
		t.Fatal(err)
	}
	for i := 1; i <= 2; i++ {
		if _, err = d.db.Exec("INSERT INTO ecu_catalog_bindings VALUES(?,1,?)", i, packedDocument(t, map[string]any{"id": i})); err != nil {
			t.Fatal(err)
		}
	}
	return d
}
func TestECUConfigurationTargetedAndOwnerIsolation(t *testing.T) {
	d := configurationFixture(t)
	ctx := context.Background()
	marker := map[string]any{"$ecu_catalog": json.Number("1")}
	got, err := d.documentPointerValue(ctx, marker, "/signals/0")
	if err != nil {
		t.Fatal(err)
	}
	if got.(map[string]any)["value"] != json.Number("18446744073709551615") {
		t.Fatal(got)
	}
	// Unrelated corrupt semantic references are not executed for selected records.
	if _, err = d.expandMetadata(ctx, marker, make(referenceStack)); err == nil {
		t.Fatal("whole expansion should detect invalid unrelated reference")
	}
	a, err := d.ecuCatalog(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := d.ecuCatalog(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a.(map[string]any)["category"], b.(map[string]any)["category"]) {
		t.Fatal("owner overwritten")
	}
	a.(map[string]any)["category"] = nil
	again, err := d.ecuCatalog(ctx, 1)
	if err != nil || again.(map[string]any)["category"] == nil {
		t.Fatal("owner alias", err)
	}
	if d.ecuConfigurationCache.member != "1" {
		t.Fatal("shared definition not cached")
	}
}
func TestECUConfigurationMissingAndCorrupt(t *testing.T) {
	for _, sql := range []string{"DELETE FROM ecu_catalog_definitions", "UPDATE ecu_catalog_bindings SET definition_id=0", "UPDATE ecu_catalog_bindings SET category=X'00'"} {
		t.Run(sql, func(t *testing.T) {
			d := configurationFixture(t)
			if _, e := d.db.Exec(sql); e != nil {
				t.Fatal(e)
			}
			if _, e := d.ecuCatalog(context.Background(), 1); e == nil {
				t.Fatal("corruption accepted")
			}
		})
	}
	d := configurationFixture(t)
	if _, e := d.ecuCatalog(context.Background(), 99); e == nil {
		t.Fatal("missing owner accepted")
	}
	if _, e := d.db.Exec("UPDATE ecu_catalog_definitions SET data=?", packedDocument(t, map[string]any{"category": nil})); e != nil {
		t.Fatal(e)
	}
	if _, e := d.ecuCatalog(context.Background(), 1); e == nil || !strings.Contains(e.Error(), "duplicates") {
		t.Fatal(e)
	}
}
