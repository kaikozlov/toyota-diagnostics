package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func supportFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	_, e := d.db.Exec(`
 CREATE TABLE text_values(id INTEGER PRIMARY KEY,value TEXT);
 INSERT INTO text_values VALUES(1,'ECU'),(2,'Support'),(3,'Group');
 CREATE TABLE support_member_facts(id INTEGER PRIMARY KEY,identifier INTEGER,list_key INTEGER,exception_handler_flag INTEGER,exception_handler_id INTEGER);
 CREATE TABLE support_member_origins(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER);
 INSERT INTO support_member_facts VALUES(1,9007199254740993,4,0,0);
 INSERT INTO support_member_origins VALUES(1,1,1,45,7);
 CREATE TABLE support_query_facts(id INTEGER PRIMARY KEY,query_id INTEGER,policy INTEGER,exception_handler_flag INTEGER,exception_handler_id INTEGER,kind_id INTEGER,requires_previous INTEGER);
 CREATE TABLE support_query_origins(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER);
 INSERT INTO support_query_facts VALUES(1,99,2,0,0,2,1);
 INSERT INTO support_query_origins VALUES(1,1,1,35,9);
 CREATE TABLE freeze_query_policies(id INTEGER PRIMARY KEY,length3_offset INTEGER,length4_offset INTEGER);
 INSERT INTO freeze_query_policies VALUES(1,2,3);
 CREATE TABLE support_freeze_facts(id INTEGER PRIMARY KEY,freeze_frame_number INTEGER,exception_handler_flag INTEGER,exception_handler_id INTEGER,policy_id INTEGER);
 CREATE TABLE support_freeze_origins(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER);
 INSERT INTO support_freeze_facts VALUES(1,5,0,0,1);
 INSERT INTO support_freeze_origins VALUES(1,1,1,36,10);
 CREATE TABLE support_group_facts(id INTEGER PRIMARY KEY,group_id INTEGER,list_key INTEGER,member_table INTEGER,name_string_index INTEGER,exception_handler_flag INTEGER,exception_handler_id INTEGER,name_id INTEGER,member_count INTEGER);
 CREATE TABLE support_group_origins(id INTEGER PRIMARY KEY,definition_id INTEGER,source_id INTEGER,table_number INTEGER,record_number INTEGER);
 INSERT INTO support_group_facts VALUES(1,77,4,45,0,0,0,3,2);
 INSERT INTO support_group_origins VALUES(1,1,1,44,2);
 CREATE TABLE support_group_links(definition_id INTEGER,position INTEGER,member_id INTEGER);
 INSERT INTO support_group_links VALUES(1,0,1),(1,1,1);
 `)
	if e != nil {
		t.Fatal(e)
	}
	return d
}
func TestSupportFactsTypedAndExact(t *testing.T) {
	d := supportFixture(t)
	ctx := context.Background()
	v, e := d.supportFacts(ctx, "$support_group_facts", 1)
	if e != nil {
		t.Fatal(e)
	}
	g := v.(map[string]any)
	a := g["members"].([]any)
	if len(a) != 2 || a[0] != json.Number("9007199254740993") || a[1] != a[0] {
		t.Fatal(g)
	}
	q, e := d.supportFacts(ctx, "$support_query_facts", 1)
	if e != nil || q.(map[string]any)["requires_previously_supported_query_id_in_p4"] != true {
		t.Fatal(q, e)
	}
	f, e := d.supportFacts(ctx, "$freeze_query_facts", 1)
	if e != nil || f.(map[string]any)["request_variable_byte_by_frame_length"].(map[string]any)["4"] != json.Number("3") {
		t.Fatal(f, e)
	}
	// Public expansion never exposes mutable cached member slices.
	root := map[string]any{"$support_group_facts": json.Number("1")}
	first, e := d.expandMetadata(ctx, root, make(referenceStack))
	if e != nil {
		t.Fatal(e)
	}
	first.(map[string]any)["members"].([]any)[0] = nil
	next, e := d.expandMetadata(ctx, root, make(referenceStack))
	if e != nil || next.(map[string]any)["members"].([]any)[0] == nil {
		t.Fatal(next, e)
	}
}
func TestSupportFactsRejectCorruption(t *testing.T) {
	for _, sql := range []string{"DELETE FROM support_group_links WHERE position=1", "UPDATE support_group_links SET position=3 WHERE position=1", "UPDATE support_member_origins SET source_id=2", "UPDATE support_member_origins SET table_number=5", "UPDATE support_member_facts SET list_key=5", "DELETE FROM support_member_facts", "DELETE FROM text_values WHERE id=3"} {
		t.Run(sql, func(t *testing.T) {
			d := supportFixture(t)
			if _, e := d.db.Exec(sql); e != nil {
				t.Fatal(e)
			}
			if _, e := d.supportFacts(context.Background(), "$support_group_facts", 1); e == nil {
				t.Fatal("accepted corrupt group")
			}
		})
	}
	d := supportFixture(t)
	d.db.Exec("UPDATE support_query_facts SET requires_previous=2")
	if _, e := d.supportFacts(context.Background(), "$support_query_facts", 1); e == nil {
		t.Fatal("accepted invalid bool")
	}
}
