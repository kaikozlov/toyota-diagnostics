package catalog

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"
)

func membershipFixture(t *testing.T) *DB {
	t.Helper()
	d := emptyDB(t)
	_, err := d.db.Exec(`CREATE TABLE signal_definitions(id INTEGER PRIMARY KEY);
 CREATE TABLE did_membership_sets(id INTEGER PRIMARY KEY,group_count INTEGER);
 CREATE TABLE did_membership_groups(set_id INTEGER,position INTEGER,key TEXT,member_count INTEGER);
 CREATE TABLE did_membership_rows(set_id INTEGER,group_position INTEGER,position INTEGER,definition_id INTEGER);
 INSERT INTO signal_definitions VALUES(9007199254740993);
 INSERT INTO did_membership_sets VALUES(1,2);
 INSERT INTO did_membership_groups VALUES(1,0,'a/~',2),(1,1,'empty',0);
 INSERT INTO did_membership_rows VALUES(1,0,0,9007199254740993),(1,0,1,9007199254740993);`)
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestMembershipPreservesGroupsOrderDuplicatesAndExactIDs(t *testing.T) {
	d := membershipFixture(t)
	v, err := d.membership(context.Background(), "$did_membership", 1)
	if err != nil {
		t.Fatal(err)
	}
	ref := map[string]any{"$signal_definition": json.Number("9007199254740993")}
	want := map[string]any{"a/~": []any{ref, ref}, "empty": []any{}}
	if !reflect.DeepEqual(v, want) {
		t.Fatalf("%#v", v)
	}
	// Fetching the collection does not reconstruct any semantic leaf: this
	// fixture deliberately contains only target identities, not their fields.
	again, err := d.restoreReference(context.Background(), "$did_membership", 1)
	if err != nil || !reflect.DeepEqual(again, want) {
		t.Fatal(again, err)
	}
}
func TestMembershipPointerSelectsNormalizedLeaf(t *testing.T) {
	d := membershipFixture(t)
	value, err := d.documentPointerValue(context.Background(), map[string]any{"$did_membership": json.Number("1")}, "/a~1~0/1")
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"$signal_definition": json.Number("9007199254740993")}
	if !reflect.DeepEqual(value, want) {
		t.Fatal(value)
	}
}
func TestMembershipCorruptionFailsClosed(t *testing.T) {
	for _, stmt := range []string{
		"DELETE FROM did_membership_groups WHERE position=1",
		"DELETE FROM did_membership_rows WHERE position=1",
		"UPDATE did_membership_rows SET position=3 WHERE position=1",
		"DELETE FROM signal_definitions",
		"UPDATE did_membership_groups SET key='a/~' WHERE position=1",
		"UPDATE did_membership_rows SET group_position=8",
		"UPDATE did_membership_groups SET member_count=-1",
	} {
		t.Run(stmt, func(t *testing.T) {
			d := membershipFixture(t)
			if _, err := d.db.Exec(stmt); err != nil {
				t.Fatal(err)
			}
			if _, err := d.membership(context.Background(), "$did_membership", 1); err == nil {
				t.Fatal("accepted corrupt membership")
			}
		})
	}
}
func TestDisplayMembership(t *testing.T) {
	d := emptyDB(t)
	_, err := d.db.Exec(`CREATE TABLE data_list_rows(id INTEGER PRIMARY KEY);INSERT INTO data_list_rows VALUES(8);
 CREATE TABLE display_membership_sets(id INTEGER PRIMARY KEY,row_count INTEGER);INSERT INTO display_membership_sets VALUES(2,2);
 CREATE TABLE display_membership_rows(set_id INTEGER,position INTEGER,row_id INTEGER);INSERT INTO display_membership_rows VALUES(2,0,8),(2,1,8);`)
	if err != nil {
		t.Fatal(err)
	}
	v, err := d.membership(context.Background(), "$display_membership", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(v.([]any)) != 2 {
		t.Fatal(v)
	}
}
