package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
)

func signalFixture(t *testing.T) *DB {
	t.Helper()
	raw, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	raw.SetMaxOpenConns(1)
	t.Cleanup(func() { raw.Close() })
	_, err = raw.Exec(`
CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
INSERT INTO text_values VALUES(1,'Voltage'),(2,'V'),(3,'p6-native-msb0-u32-v1'),(4,'fixed'),(5,'Engine.ddb'),(6,'0x1002'),(7,'p5-linear-msb0-v1'),(8,'0'),(9,'No'),(10,'1'),(11,'Yes');
CREATE TABLE signal_labels(id INTEGER PRIMARY KEY,monitor_key INTEGER,name_id INTEGER,bit_start INTEGER,bit_end INTEGER);
INSERT INTO signal_labels VALUES(1,9007199254740993,1,0,15);
CREATE TABLE signal_pattern_sets(id INTEGER PRIMARY KEY,entry_count INTEGER);
INSERT INTO signal_pattern_sets VALUES(1,0),(2,2);
CREATE TABLE signal_pattern_entries(set_id INTEGER,key_id INTEGER,value_id INTEGER,PRIMARY KEY(set_id,key_id));
INSERT INTO signal_pattern_entries VALUES(2,8,9),(2,10,11);
CREATE TABLE signal_infos(id INTEGER PRIMARY KEY,bit_width INTEGER,decimal_point_count INTEGER,div INTEGER,mul INTEGER,offset INTEGER,physical_data_key INTEGER,sign_mode INTEGER,unit_id INTEGER,unit_key INTEGER,pattern_id INTEGER,signed_view INTEGER);
INSERT INTO signal_infos VALUES(1,16,3,512,625,0,115,0,2,7,1,0);
CREATE TABLE signal_did_support(id INTEGER PRIMARY KEY,did INTEGER,fixed_length INTEGER,record INTEGER,encoding_id INTEGER,support_mode INTEGER,table_type INTEGER,has_additional INTEGER,additional_count INTEGER);
INSERT INTO signal_did_support VALUES(1,4098,2,0,4,0,61,0,0);
CREATE TABLE signal_support_additional(parent_id INTEGER,position INTEGER,child_id INTEGER,PRIMARY KEY(parent_id,position));
CREATE TABLE signal_conditions(id INTEGER PRIMARY KEY,bit_end INTEGER,bit_start INTEGER,comparison_mode INTEGER,comparison_value INTEGER,condition_type INTEGER,condition_key INTEGER,record INTEGER,referenced_did INTEGER,table_type INTEGER,variable_bytes BLOB,variable_id INTEGER,condition_view INTEGER);
CREATE TABLE signal_monitors(id INTEGER PRIMARY KEY,label_id INTEGER,alternate_did INTEGER,info_id INTEGER,support_id INTEGER,condition_id INTEGER,extraction_mode INTEGER,flags INTEGER,graph_low INTEGER,graph_high INTEGER,list_member INTEGER,pattern_display_key INTEGER,physical_data_key INTEGER,physical_low REAL,physical_high REAL,primary_did INTEGER,record INTEGER,sort_key INTEGER,source_id INTEGER,table_type INTEGER,visibility_override INTEGER);
INSERT INTO signal_monitors VALUES(1,1,4098,1,1,NULL,2,67,0,79997,1,0,115,0.0,79.998,4098,0,210,5,62,0);
CREATE TABLE signal_definitions(id INTEGER PRIMARY KEY,generation INTEGER,label_id INTEGER,alternate_did INTEGER,decimal_point_count INTEGER,decoder_id INTEGER,div INTEGER,mul INTEGER,offset INTEGER,unit_id INTEGER,graph_low INTEGER,graph_high INTEGER,data_low INTEGER,data_high INTEGER,pattern_id INTEGER,signed INTEGER,monitor_id INTEGER);
INSERT INTO signal_definitions VALUES(1,6,NULL,NULL,NULL,3,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,NULL,1);
INSERT INTO signal_definitions VALUES(2,5,1,1026,0,7,1,1,0,NULL,0,65535,0,65535,2,0,NULL);
CREATE TABLE data_list_rows(id INTEGER PRIMARY KEY,generation INTEGER,label_id INTEGER,sort_key INTEGER,monitor_id INTEGER,did_id INTEGER,wire_field INTEGER);
INSERT INTO data_list_rows VALUES(1,6,NULL,NULL,1,6,1),(2,5,1,0,NULL,6,NULL);
`)
	if err != nil {
		t.Fatal(err)
	}
	return &DB{db: raw}
}

func TestTypedSignalsReconstructViews(t *testing.T) {
	d := signalFixture(t)
	ctx := context.Background()
	value, err := d.signalDefinition(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	signal := value.(map[string]any)
	info := signal["signal_info"].(map[string]any)
	for _, key := range []string{"div", "mul", "offset", "unit", "bit_width", "decimal_point_count", "sign_mode"} {
		if signal[key] != info[key] {
			t.Fatalf("projection mismatch %s", key)
		}
	}
	encoded, err := json.Marshal(signal)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), `"monitor_key":9007199254740993`) {
		t.Fatalf("integer lost: %s", encoded)
	}
	if !strings.Contains(string(encoded), `"physical_range":[0.0,79.998]`) {
		t.Fatalf("float type lost: %s", encoded)
	}
	display, err := d.dataListRow(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	row := display.(map[string]any)
	if row["did"] != "0x1002" || row["wire_field"] != true || len(row["composite_records"].([]any)) != 0 {
		t.Fatalf("bad display: %#v", row)
	}
	if _, ok := row["decoder"]; ok {
		t.Fatal("display gained definition fields")
	}
	legacy, err := d.signalDefinition(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.(map[string]any)["unit"] != nil || legacy.(map[string]any)["signed"] != false {
		t.Fatalf("bad nullable legacy fields")
	}
	patterns := legacy.(map[string]any)["patterns"].(map[string]any)
	if patterns["0"] != "No" || patterns["1"] != "Yes" {
		t.Fatalf("bad patterns: %#v", patterns)
	}
	if _, err := d.dataListRow(ctx, 2); err != nil {
		t.Fatal(err)
	}
}

func TestTypedSignalReferencesExpandAndRemainIndependent(t *testing.T) {
	d := signalFixture(t)
	ctx := context.Background()
	marker := map[string]any{"$signal_definition": json.Number("1")}
	first, err := d.expandMetadata(ctx, marker, make(referenceStack))
	if err != nil {
		t.Fatal(err)
	}
	first.(map[string]any)["signal_info"].(map[string]any)["mul"] = json.Number("999")
	second, err := d.expandMetadata(ctx, marker, make(referenceStack))
	if err != nil {
		t.Fatal(err)
	}
	if second.(map[string]any)["signal_info"].(map[string]any)["mul"] != json.Number("625") {
		t.Fatal("cached entity was mutated")
	}
	runtime, err := d.runtimeValue(ctx, marker)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.(map[string]any)["mul"] != float64(625) {
		t.Fatal("runtime signal stayed lazy")
	}
	for _, entry := range []struct {
		kind string
		id   string
	}{{"$signal_info", "1"}, {"$did_support", "1"}, {"$data_list_row", "1"}} {
		value, err := d.expandMetadata(ctx, map[string]any{entry.kind: json.Number(entry.id)}, make(referenceStack))
		if err != nil {
			t.Fatal(err)
		}
		packed, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(packed), entry.kind) {
			t.Fatalf("marker leaked: %s", packed)
		}
	}
}

func TestTypedLegacyInfoAndUnresolvedLabels(t *testing.T) {
	d := signalFixture(t)
	ctx := context.Background()
	if _, err := d.db.Exec("INSERT INTO signal_infos VALUES(2,16,3,512,625,0,115,1,2,7,2,1)"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.db.Exec("UPDATE signal_pattern_entries SET value_id=NULL WHERE set_id=2 AND key_id=8"); err != nil {
		t.Fatal(err)
	}
	value, err := d.signalInfo(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	info := value.(map[string]any)
	if info["signed"] != true {
		t.Fatal("legacy signed flag lost")
	}
	if _, ok := info["sign_mode"]; ok {
		t.Fatal("legacy view gained numeric sign mode")
	}
	labels := info["pattern_display"].(map[string]any)
	if label, ok := labels["0"]; !ok || label != nil {
		t.Fatal("unresolved label lost")
	}
	if labels["1"] != "Yes" {
		t.Fatal("resolved label lost")
	}
}

func TestTypedConditionProjectionShapes(t *testing.T) {
	d := signalFixture(t)
	ctx := context.Background()
	for view := 0; view < 3; view++ {
		if _, err := d.db.Exec("INSERT INTO signal_conditions VALUES(?,7,7,0,0,7,1,0,1027,63,x'0403',11290,?)", view+1, view); err != nil {
			t.Fatal(err)
		}
		value, err := d.supportCondition(ctx, int64(view+1))
		if err != nil {
			t.Fatal(err)
		}
		row := value.(map[string]any)
		_, comparison := row["comparison_mode"]
		_, record := row["record"]
		if comparison != (view == 0) || record != (view != 2) {
			t.Fatalf("wrong condition view %d: %#v", view, row)
		}
		if row["referenced_did"] != json.Number("1027") {
			t.Fatal("condition value lost")
		}
	}
}

func TestTypedSignalCacheNamespacesAndLargeIDs(t *testing.T) {
	d := signalFixture(t)
	ctx := context.Background()
	info, err := d.signalInfo(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	support, err := d.didSupport(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if info.(map[string]any)["bit_width"] == nil || support.(map[string]any)["fixed_length"] == nil {
		t.Fatal("typed cache namespaces collided")
	}
	const largeID = int64(1<<55) + 1
	if _, err := d.db.Exec("UPDATE signal_definitions SET id=? WHERE id=2", largeID); err != nil {
		t.Fatal(err)
	}
	modern, err := d.signalDefinition(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	legacy, err := d.signalDefinition(ctx, largeID)
	if err != nil {
		t.Fatal(err)
	}
	if modern.(map[string]any)["decoder"] == legacy.(map[string]any)["decoder"] {
		t.Fatal("large ID collided with cached definition")
	}
}

func TestTypedSignalsMissingAndCorruptReferences(t *testing.T) {
	t.Run("missing", func(t *testing.T) {
		d := signalFixture(t)
		if _, err := d.signalDefinition(context.Background(), 99); err == nil {
			t.Fatal("missing record accepted")
		}
	})
	t.Run("bad bool", func(t *testing.T) {
		d := signalFixture(t)
		d.db.Exec("UPDATE signal_monitors SET list_member=2")
		if _, err := d.signalDefinition(context.Background(), 1); err == nil {
			t.Fatal("invalid boolean accepted")
		}
	})
	t.Run("support gap", func(t *testing.T) {
		d := signalFixture(t)
		d.db.Exec("UPDATE signal_did_support SET has_additional=1,additional_count=1")
		d.db.Exec("INSERT INTO signal_support_additional VALUES(1,2,1)")
		if _, err := d.signalDefinition(context.Background(), 1); err == nil {
			t.Fatal("support gap accepted")
		}
	})
	t.Run("support cycle", func(t *testing.T) {
		d := signalFixture(t)
		d.db.Exec("UPDATE signal_did_support SET has_additional=1,additional_count=1")
		d.db.Exec("INSERT INTO signal_support_additional VALUES(1,0,1)")
		if _, err := d.signalDefinition(context.Background(), 1); err == nil {
			t.Fatal("support cycle accepted")
		}
	})
	t.Run("missing pattern entry", func(t *testing.T) {
		d := signalFixture(t)
		d.db.Exec("DELETE FROM signal_pattern_entries WHERE key_id=8")
		if _, err := d.signalDefinition(context.Background(), 2); err == nil {
			t.Fatal("missing pattern entry accepted")
		}
	})
	t.Run("missing support child", func(t *testing.T) {
		d := signalFixture(t)
		d.db.Exec("UPDATE signal_did_support SET has_additional=1,additional_count=1")
		if _, err := d.signalDefinition(context.Background(), 1); err == nil {
			t.Fatal("missing support child accepted")
		}
	})
	t.Run("generation", func(t *testing.T) {
		d := signalFixture(t)
		d.db.Exec("UPDATE signal_definitions SET generation=7")
		if _, err := d.signalDefinition(context.Background(), 1); err == nil {
			t.Fatal("unknown generation accepted")
		}
	})
}
