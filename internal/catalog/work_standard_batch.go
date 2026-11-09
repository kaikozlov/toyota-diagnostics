package catalog

import (
	"context"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Standard-value arrays are complete semantic collections, often thousands of
// independent entries. Read bounded batches rather than issuing one query per
// entry or retaining whole documents in an enlarged global cache.
func workColumns(fields []workSemanticField, base, prefix string) ([]string, []string) {
	columns, joins := []string{}, []string{}
	for i, field := range fields {
		if field.kind == 't' {
			alias := fmt.Sprintf("%s%d", prefix, i)
			columns = append(columns, alias+".value")
			joins = append(joins, "LEFT JOIN text_values "+alias+" ON "+alias+".id="+base+"."+field.column)
		} else {
			columns = append(columns, base+"."+field.column)
		}
	}
	return columns, joins
}
func workDecodedFields(fields []workSemanticField, values []any) (map[string]any, error) {
	result := map[string]any{}
	for i, field := range fields {
		value := values[i]
		if field.kind >= '1' && field.kind <= '4' {
			if value == nil {
				continue
			}
			blob, ok := value.([]byte)
			width := int(field.kind - '0')
			if width == 4 {
				width = 3
			}
			if !ok || len(blob) != width {
				return nil, fmt.Errorf("invalid work-factor unknown bytes")
			}
			unknown := map[string]any{}
			switch field.kind {
			case '1':
				unknown["11"] = hex.EncodeToString(blob)
			case '2':
				unknown["10"] = hex.EncodeToString(blob)
			case '3':
				unknown["17"] = hex.EncodeToString(blob)
			case '4':
				unknown["16"] = hex.EncodeToString(blob[:2])
				unknown["19"] = hex.EncodeToString(blob[2:])
			}
			result[field.name] = unknown
			continue
		}
		if field.kind == 't' {
			if value != nil {
				if _, ok := value.(string); !ok {
					return nil, fmt.Errorf("invalid work-factor text")
				}
			}
			result[field.name] = value
			continue
		}
		number, ok := value.(int64)
		if !ok {
			return nil, fmt.Errorf("invalid work-factor integer")
		}
		if field.kind == 'b' {
			if number != 0 && number != 1 {
				return nil, fmt.Errorf("invalid translation state")
			}
			result[field.name] = number == 1
		} else {
			result[field.name] = json.Number(strconv.FormatInt(number, 10))
		}
	}
	return result, nil
}
func (d *DB) workBatchStatement(ctx context.Context, query string) (*sql.Stmt, error) {
	d.workStatements.mu.Lock()
	defer d.workStatements.mu.Unlock()
	const key = "work_standard_batch_256"
	if stmt := d.workStatements.statements[key]; stmt != nil {
		return stmt, nil
	}
	stmt, err := d.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	if d.workStatements.statements == nil {
		d.workStatements.statements = map[string]*sql.Stmt{}
	}
	d.workStatements.statements[key] = stmt
	return stmt, nil
}
func (d *DB) workStandardArray(ctx context.Context, nodes []any, active referenceStack) ([]any, bool, error) {
	if len(nodes) < 32 {
		return nil, false, nil
	}
	ids := make([]int64, len(nodes))
	for i, node := range nodes {
		ref, ok := node.(map[string]any)
		if !ok || len(ref) != 1 {
			return nil, false, nil
		}
		value, ok := ref["$work_standard"]
		if !ok {
			return nil, false, nil
		}
		id, err := referenceID(value, "$work_standard")
		if err != nil {
			return nil, true, err
		}
		if active[referenceKey{"$work_standard", id}] {
			return nil, true, fmt.Errorf("cyclic work-standard reference %d", id)
		}
		ids[i] = id
	}
	result := make([]any, len(ids))
	standard, joins := workColumns(workStandardFields, "d", "s")
	help, helpJoins := workColumns(workHelpFields, "h", "t")
	columns := append([]string{"d.id"}, standard...)
	columns = append(columns, "d.help_definition_id", "h.id")
	columns = append(columns, help...)
	joins = append(joins, "LEFT JOIN work_help_definitions h ON h.id=d.help_definition_id")
	joins = append(joins, helpJoins...)
	for start := 0; start < len(ids); start += 256 {
		end := start + 256
		if end > len(ids) {
			end = len(ids)
		}
		args := make([]any, end-start)
		marks := make([]string, len(args))
		for i := range args {
			args[i] = ids[start+i]
			marks[i] = "?"
		}
		query := "SELECT " + strings.Join(columns, ",") + " FROM work_standard_definitions d " + strings.Join(joins, " ") + " WHERE d.id IN(" + strings.Join(marks, ",") + ")"
		var rows *sql.Rows
		var err error
		if len(args) == 256 {
			stmt, e := d.workBatchStatement(ctx, query)
			if e != nil {
				return nil, true, e
			}
			rows, err = stmt.QueryContext(ctx, args...)
		} else {
			rows, err = d.db.QueryContext(ctx, query, args...)
		}
		if err != nil {
			return nil, true, err
		}
		found := map[int64]any{}
		for rows.Next() {
			values := make([]any, len(columns))
			dest := make([]any, len(values))
			for i := range dest {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, true, err
			}
			id, ok := values[0].(int64)
			if !ok {
				rows.Close()
				return nil, true, fmt.Errorf("invalid standard ID")
			}
			value, e := workDecodedFields(workStandardFields, values[1:])
			if e != nil {
				rows.Close()
				return nil, true, e
			}
			offset := 1 + len(workStandardFields)
			helpers := []any{}
			if values[offset] != nil {
				if values[offset+1] == nil {
					rows.Close()
					return nil, true, fmt.Errorf("missing work-factor help")
				}
				h, e := workDecodedFields(workHelpFields, values[offset+2:])
				if e != nil {
					rows.Close()
					return nil, true, e
				}
				helpers = append(helpers, h)
			}
			value["standard_value_help_records"] = helpers
			found[id] = value
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, true, err
		}
		for i := start; i < end; i++ {
			value, ok := found[ids[i]]
			if !ok {
				return nil, true, fmt.Errorf("missing work-standard definition %d", ids[i])
			}
			result[i] = value
		}
	}
	return result, true, nil
}
