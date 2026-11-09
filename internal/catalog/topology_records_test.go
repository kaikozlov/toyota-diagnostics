package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func topologyFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	_, err := d.db.Exec(`
 CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 CREATE TABLE vehicle_definitions(id INTEGER PRIMARY KEY,vehicle_type INTEGER,name_id INTEGER);
 CREATE TABLE topology_vehicle_views(id INTEGER PRIMARY KEY,vehicle_id INTEGER,network_id INTEGER);
 CREATE TABLE topology_networks(id INTEGER PRIMARY KEY,can_car_id INTEGER,option_count INTEGER,variant_count INTEGER);
 CREATE TABLE topology_network_variants(network_id INTEGER,position INTEGER,variant_id INTEGER);
 CREATE TABLE topology_variants(id INTEGER PRIMARY KEY,group_count INTEGER,placement_count INTEGER);
 CREATE TABLE topology_variant_groups(variant_id INTEGER,position INTEGER,group_id INTEGER);
 CREATE TABLE topology_variant_placements(variant_id INTEGER,position INTEGER,placement_id INTEGER);
 CREATE TABLE topology_placements(id INTEGER PRIMARY KEY,bus_index INTEGER,component_index INTEGER,domain_id INTEGER,junction_id INTEGER,gateway_count INTEGER);
 CREATE TABLE topology_buses(bus_index INTEGER PRIMARY KEY,name_id INTEGER);
 CREATE TABLE topology_gateways(placement_id INTEGER,position INTEGER,name_id INTEGER);
 INSERT INTO text_values VALUES(1,'Toyota'),(2,'Bus 1'),(3,'Radar'),(4,'Junction'),(5,'Gateway');
 INSERT INTO vehicle_definitions VALUES(1,7,1),(2,8,1);
 INSERT INTO topology_vehicle_views VALUES(1,1,1),(2,2,1);
 INSERT INTO topology_networks VALUES(1,1,2,1);
 INSERT INTO topology_network_variants VALUES(1,0,1);
 INSERT INTO topology_variants VALUES(1,1,1);
 INSERT INTO topology_variant_groups VALUES(1,0,3);
 INSERT INTO topology_variant_placements VALUES(1,0,1);
 INSERT INTO topology_placements VALUES(1,23,1,3,4,1);
 INSERT INTO topology_buses VALUES(23,2);
 INSERT INTO topology_gateways VALUES(1,0,5);
 `)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestTopologySourceViewAndIsolation(t *testing.T) {
	d := topologyFixture(t)
	ctx := context.Background()
	value, err := d.expandMetadata(ctx, map[string]any{"$can_topology": json.Number("1")}, make(referenceStack))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"can_bus_car_id":"0x00000001"`, `"component_groups":["0x00000003"]`, `"component_hex":"0x01"`, `"vehicle_type":7`, `"gateway_names":["Gateway"]`} {
		if !strings.Contains(string(raw), want) {
			t.Fatalf("%s missing%s", raw, want)
		}
	}
	value.(map[string]any)["vehicle_name"] = "mutated"
	next, err := d.expandMetadata(ctx, map[string]any{"$can_topology": json.Number("2")}, make(referenceStack))
	if err != nil {
		t.Fatal(err)
	}
	if next.(map[string]any)["vehicle_name"] != "Toyota" || next.(map[string]any)["vehicle_type"] != json.Number("8") {
		t.Fatalf("%v", next)
	}
}
func TestTopologyMissingAssociations(t *testing.T) {
	for _, table := range []string{"topology_gateways", "topology_variant_placements", "topology_variant_groups", "topology_network_variants"} {
		t.Run(table, func(t *testing.T) {
			d := topologyFixture(t)
			if _, err := d.db.Exec("DELETE FROM " + table); err != nil {
				t.Fatal(err)
			}
			if _, err := d.canTopology(context.Background(), 1); err == nil {
				t.Fatal("missing trailing association accepted")
			}
		})
	}
}
