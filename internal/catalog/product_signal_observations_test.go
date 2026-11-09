package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func observationFixture(t *testing.T) *DB {
	t.Helper()
	d := signalFixture(t)
	cols := []string{}
	for _, k := range observationIntegers {
		cols = append(cols, k+" INTEGER")
	}
	ddl := fmt.Sprintf(`CREATE TABLE signal_observations(id INTEGER PRIMARY KEY,shape INTEGER,name_id INTEGER,decoder_id INTEGER,info_id INTEGER,support_id INTEGER,condition_id INTEGER,dynamic_lsb INTEGER,%s,physical_low REAL,physical_high REAL,graph_low INTEGER,graph_high INTEGER,auxiliary_tables INTEGER);
 CREATE TABLE signal_observation_entries(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER);
 INSERT INTO signal_observations(id,shape,name_id,info_id,dynamic_lsb,bit_end,bit_start,local_support_mode,pattern_display_key,physical_data_key,sort_key,flags,monitor_key,primary_did,snapshot_did) VALUES(1,4,1,1,0,15,0,NULL,0,1,3,2,5,258,1026);
 INSERT INTO signal_observation_entries VALUES(1,1,5,62,7);`, strings.Join(cols, ","))
	if _, err := d.db.Exec(ddl); err != nil {
		t.Fatal(err)
	}
	return d
}
func TestSignalObservationSemanticFields(t *testing.T) {
	d := observationFixture(t)
	ctx := context.Background()
	value, err := d.signalObservation(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	row := value.(map[string]any)
	if row["local_support_mode"] != nil || row["dynamic_lsb_possible"] != false || row["primary_did"] != json.Number("258") {
		t.Fatal(row)
	}
	runtime, err := d.runtimeValue(ctx, map[string]any{"$signal_observation": json.Number("1")})
	if err != nil {
		t.Fatal(err)
	}
	r := runtime.(map[string]any)
	if r["primary_did"] != float64(258) || r["signal_info"] == nil {
		t.Fatal(r)
	}
	r["name"] = "mutated"
	again, err := d.signalObservation(ctx, 1)
	if err != nil || again.(map[string]any)["name"] == "mutated" {
		t.Fatal("cache mutated", err)
	}
}
func TestSignalObservationCorruption(t *testing.T) {
	d := observationFixture(t)
	ctx := context.Background()
	if _, err := d.db.Exec("UPDATE signal_observations SET shape=99"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.signalObservation(ctx, 1); err == nil {
		t.Fatal("accepted unknown shape")
	}
	if _, err := d.db.Exec("UPDATE signal_observations SET shape=4,bit_end=NULL"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.signalObservation(ctx, 1); err == nil {
		t.Fatal("accepted missing geometry")
	}
}
