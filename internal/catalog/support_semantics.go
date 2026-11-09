package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

var supportFields = map[string][]string{
	"member": {"identifier", "list_key", "exception_handler_flag", "exception_handler_id"},
	"query":  {"query_id", "policy", "exception_handler_flag", "exception_handler_id"},
	"freeze": {"freeze_frame_number", "exception_handler_flag", "exception_handler_id"},
	"group":  {"group_id", "list_key", "member_table", "name_string_index", "exception_handler_flag", "exception_handler_id"},
}

// Prepared queries are fixed by four named semantic schemas, not supplied data.
func (d *DB) supportStatement(ctx context.Context, kind string) (*sql.Stmt, error) {
	c := &d.workStatements
	c.mu.Lock()
	defer c.mu.Unlock()
	key := "support:" + kind
	if s := c.statements[key]; s != nil {
		return s, nil
	}
	cols := []string{"o.definition_id", "s.value", "o.table_number", "o.record_number"}
	for _, f := range supportFields[kind] {
		cols = append(cols, "d."+f)
	}
	joins := ""
	switch kind {
	case "query":
		cols = append(cols, "t.value", "d.requires_previous")
		joins = " LEFT JOIN text_values t ON t.id=d.kind_id"
	case "freeze":
		cols = append(cols, "p.length3_offset", "p.length4_offset")
		joins = " LEFT JOIN freeze_query_policies p ON p.id=d.policy_id"
	case "group":
		cols = append(cols, "t.value", "d.member_count")
		joins = " LEFT JOIN text_values t ON t.id=d.name_id"
	}
	q := "SELECT " + strings.Join(cols, ",") + " FROM support_" + kind + "_origins o JOIN support_" + kind + "_facts d ON d.id=o.definition_id LEFT JOIN text_values s ON s.id=o.source_id" + joins + " WHERE o.id=?"
	stmt, err := d.db.PrepareContext(ctx, q)
	if err != nil {
		return nil, err
	}
	if c.statements == nil {
		c.statements = map[string]*sql.Stmt{}
	}
	c.statements[key] = stmt
	return stmt, nil
}
func (d *DB) supportFacts(ctx context.Context, marker string, id int64) (any, error) {
	var kind string
	var cache *cachedMetadata
	switch marker {
	case "$support_member_facts":
		kind = "member"
		cache = &d.supportMembers
	case "$support_query_facts":
		kind = "query"
		cache = &d.supportQueries
	case "$freeze_query_facts":
		kind = "freeze"
		cache = &d.supportFreezeQueries
	case "$support_group_facts":
		kind = "group"
		cache = &d.supportGroups
	default:
		return nil, fmt.Errorf("unknown support reference")
	}
	if id <= 0 {
		return nil, fmt.Errorf("invalid support reference")
	}
	if v, ok := cache.load(id); ok {
		return v, nil
	}
	stmt, err := d.supportStatement(ctx, kind)
	if err != nil {
		return nil, err
	}
	fields := supportFields[kind]
	n := 4 + len(fields)
	if kind != "member" {
		n += 2
	}
	values := make([]any, n)
	dest := make([]any, n)
	for i := range dest {
		dest[i] = &values[i]
	}
	if err = stmt.QueryRowContext(ctx, id).Scan(dest...); err != nil {
		return nil, err
	}
	definition, ok := values[0].(int64)
	if !ok || definition <= 0 {
		return nil, fmt.Errorf("invalid support definition")
	}
	source, ok := values[1].(string)
	if !ok {
		return nil, fmt.Errorf("missing support source")
	}
	out := map[string]any{"source": source}
	for i, k := range append([]string{"table", "record"}, fields...) {
		v, ok := values[i+2].(int64)
		if !ok {
			return nil, fmt.Errorf("invalid support integer %s", k)
		}
		out[k] = catalogNumber(v)
	}
	extra := values[4+len(fields):]
	switch kind {
	case "query":
		name, ok := extra[0].(string)
		if !ok {
			return nil, fmt.Errorf("missing support kind")
		}
		flag, ok := extra[1].(int64)
		if !ok || (flag != 0 && flag != 1) {
			return nil, fmt.Errorf("invalid support query flag")
		}
		out["kind"] = name
		out["requires_previously_supported_query_id_in_p4"] = flag == 1
	case "freeze":
		policy := map[string]any{}
		for i, k := range []string{"3", "4"} {
			v, ok := extra[i].(int64)
			if !ok {
				return nil, fmt.Errorf("missing freeze query policy")
			}
			policy[k] = catalogNumber(v)
		}
		out["request_variable_byte_by_frame_length"] = policy
	case "group":
		name, ok := extra[0].(string)
		if !ok {
			return nil, fmt.Errorf("missing support group name")
		}
		count, ok := extra[1].(int64)
		if !ok || count < 0 {
			return nil, fmt.Errorf("invalid support group count")
		}
		rows, err := d.db.QueryContext(ctx, `SELECT l.position,s.value,o.table_number,o.record_number,m.identifier,m.list_key FROM support_group_links l LEFT JOIN support_member_origins o ON o.id=l.member_id LEFT JOIN support_member_facts m ON m.id=o.definition_id LEFT JOIN text_values s ON s.id=o.source_id WHERE l.definition_id=? ORDER BY l.position`, definition)
		if err != nil {
			return nil, err
		}
		members, records := []any{}, []any{}
		for rows.Next() {
			var pos, table, record, identifier, key int64
			var childSource string
			if err = rows.Scan(&pos, &childSource, &table, &record, &identifier, &key); err != nil {
				rows.Close()
				return nil, err
			}
			if pos != int64(len(members)) || childSource != source || catalogNumber(table) != out["member_table"] || catalogNumber(key) != out["list_key"] {
				rows.Close()
				return nil, fmt.Errorf("inconsistent support group membership")
			}
			members = append(members, catalogNumber(identifier))
			records = append(records, catalogNumber(record))
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if int64(len(members)) != count {
			return nil, fmt.Errorf("missing support group member")
		}
		out["name"] = name
		out["members"] = members
		out["member_records"] = records
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	cache.store(id, out, len(raw))
	return out, nil
}
