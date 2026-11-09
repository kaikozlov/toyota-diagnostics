package catalog

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func vehicleIndexFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	_, err := d.db.Exec(`
 CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 INSERT INTO text_values VALUES(1,''),(2,'unrecovered'),(3,'unknown');
 CREATE TABLE vehicle_definitions(id INTEGER PRIMARY KEY,vehicle_type INTEGER,name_id INTEGER,install_count INTEGER);
 INSERT INTO vehicle_definitions VALUES(1,7,1,3);
 CREATE TABLE vehicle_install_references(vehicle_id INTEGER,position INTEGER,install_set_id INTEGER);
 INSERT INTO vehicle_install_references VALUES(1,0,10),(1,1,999),(1,2,10);
 CREATE TABLE install_set_definitions(id INTEGER PRIMARY KEY,member_count INTEGER);
 INSERT INTO install_set_definitions VALUES(1,3),(2,0);
 CREATE TABLE vehicle_connections(id INTEGER PRIMARY KEY,category_id INTEGER,comm_set_id INTEGER,frame_id INTEGER,route_selector INTEGER,has_route INTEGER);
 INSERT INTO vehicle_connections VALUES(1,10,9,0,17,1),(2,11,1,1,17,0);
 CREATE TABLE install_set_members(definition_id INTEGER,position INTEGER,connection_id INTEGER);
 INSERT INTO install_set_members VALUES(1,0,1),(1,1,1),(1,2,2);
 CREATE TABLE vehicle_route_definitions(id INTEGER PRIMARY KEY,address_extension INTEGER,change_comm_if_case INTEGER,functional_address INTEGER,functional_address_field INTEGER,
 legislated_request_address INTEGER,protocol_info_id INTEGER,request_address INTEGER,request_address_field INTEGER,request_mask INTEGER,response_mask INTEGER,route_selector INTEGER,
 functional_request_address INTEGER,physical_request_address INTEGER,physical_response_address INTEGER,controller_id INTEGER,request_address_field_semantics_id INTEGER,transport_kind_id INTEGER);
 INSERT INTO vehicle_route_definitions VALUES(1,0,1,0,0,0,77,24,24,8,8,17,NULL,NULL,NULL,NULL,2,3);
 `)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestVehicleIndexTypedReaders(t *testing.T) {
	d := vehicleIndexFixture(t)
	ctx := context.Background()
	vehicle, err := d.vehicleDefinition(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	refs := vehicle.(map[string]any)["install_set_ids"].([]any)
	if len(refs) != 3 || refs[1] != json.Number("999") || refs[0] != refs[2] {
		t.Fatalf("%v", refs)
	}
	install, err := d.installSet(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	members := install.([]any)
	if len(members) != 3 || members[0].(map[string]any)["route_key"] != "10:17" || members[2].(map[string]any)["route_key"] != nil {
		t.Fatalf("%v", members)
	}
	empty, err := d.installSet(ctx, 2)
	if err != nil || len(empty.([]any)) != 0 {
		t.Fatalf("%v %v", empty, err)
	}
	route, err := d.vehicleRoute(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	r := route.(map[string]any)
	if len(r) != 17 || r["controller"] != nil || r["physical_request_address"] != nil || r["request_address"] != json.Number("24") {
		t.Fatalf("%v", r)
	}
	// ID1 in each table must occupy distinct cache entries.
	again, err := d.vehicleDefinition(ctx, 1)
	if err != nil || again.(map[string]any)["vehicle_type"] != json.Number("7") {
		t.Fatalf("%v %v", again, err)
	}
}

func TestVehicleIndexExpansionAndRuntimeAccess(t *testing.T) {
	d := vehicleIndexFixture(t)
	ctx := context.Background()
	ref := map[string]any{"$vehicle_definition": json.Number("1")}
	value, err := d.runtimeValue(ctx, ref)
	if err != nil {
		t.Fatal(err)
	}
	vehicle, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("runtime entity is deferred: %T", value)
	}
	if vehicle["vehicle_type"] != float64(7) {
		t.Fatalf("%v", vehicle)
	}
	memberRef := map[string]any{"$install_set": json.Number("1")}
	member, err := d.documentPointerValue(ctx, memberRef, "/1/route_key")
	if err != nil || member != "10:17" {
		t.Fatalf("%v %v", member, err)
	}
	expanded, err := d.expandMetadata(ctx, memberRef, referenceStack{})
	if err != nil {
		t.Fatal(err)
	}
	members := expanded.([]any)
	members[0].(map[string]any)["category_id"] = json.Number("999")
	if members[1].(map[string]any)["category_id"] != json.Number("10") {
		t.Fatal("duplicate source memberships alias")
	}
	again, err := d.expandMetadata(ctx, memberRef, referenceStack{})
	if err != nil {
		t.Fatal(err)
	}
	if again.([]any)[0].(map[string]any)["category_id"] != json.Number("10") {
		t.Fatal("cache was mutated")
	}
}

func TestVehicleIndexCorruptMembers(t *testing.T) {
	for _, tc := range []struct {
		sql  string
		read func(*DB) (any, error)
	}{
		{"DELETE FROM vehicle_install_references WHERE position=2", func(d *DB) (any, error) { return d.vehicleDefinition(context.Background(), 1) }},
		{"UPDATE install_set_members SET position=9 WHERE position=1", func(d *DB) (any, error) { return d.installSet(context.Background(), 1) }},
		{"DELETE FROM vehicle_connections WHERE id=2", func(d *DB) (any, error) { return d.installSet(context.Background(), 1) }},
		{"UPDATE vehicle_connections SET has_route=3 WHERE id=1", func(d *DB) (any, error) { return d.installSet(context.Background(), 1) }},
	} {
		t.Run(tc.sql, func(t *testing.T) {
			d := vehicleIndexFixture(t)
			if _, err := d.db.Exec(tc.sql); err != nil {
				t.Fatal(err)
			}
			if _, err := tc.read(d); err == nil {
				t.Fatal("accepted corrupt entity")
			}
		})
	}
	d := vehicleIndexFixture(t)
	if _, err := d.vehicleRoute(context.Background(), 999); err == nil {
		t.Fatal("accepted missing route")
	}
	if _, err := d.installSet(context.Background(), 0); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("%v", err)
	}
}
