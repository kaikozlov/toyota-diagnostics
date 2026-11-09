package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
)

type workSemanticField struct {
	name, column string
	kind         byte
}

func wf(name string) workSemanticField         { return workSemanticField{name, name, 'n'} }
func wt(name, column string) workSemanticField { return workSemanticField{name, column, 't'} }

var workHelpFields = []workSemanticField{wf("help_id"), wf("kind"), wt("namespace", "namespace_id"), wf("record"), wt("source", "source_id"), wf("string_index"), wt("string_source", "string_source_id"), {"table", "table_type", 'n'}, wt("text", "text_id"), {"translation_available", "translation_available", 'b'}}
var workStandardFields = []workSemanticField{wf("exception_handler_flag"), wf("exception_handler_id"), wf("help_id"), wf("record"), wf("signal_id"), wt("source", "source_id"), wt("standard_value", "standard_value_id_text"), wf("standard_value_id"), wt("standard_value_status", "standard_value_status_id"), {"table", "table_type", 'n'}}
var workPatternFields = []workSemanticField{wf("display_code"), wf("exception_handler_flag"), wf("exception_handler_id"), wt("help", "help_id_text"), wf("help_string_index"), wt("name", "name_id"), wf("name_string_index"), wf("pattern_id"), wf("record"), wt("source", "source_id"), {"table", "table_type", 'n'}, wf("value")}
var workFactorFields = []workSemanticField{wf("exception_handler_flag"), wf("exception_handler_id"), wt("help", "help_id_text"), wf("help_string_index"), wt("name", "name_id"), wf("name_string_index"), wf("record"), wf("signal_id"), wt("source", "source_id"), {"table", "table_type", 'n'}, wf("value")}

func init() {
	workStandardFields = append(workStandardFields, workSemanticField{"uninterpreted_bytes", "unknown_bytes", '1'})
	workHelpFields = append(workHelpFields, workSemanticField{"uninterpreted_bytes", "unknown_bytes", '2'})
	workPatternFields = append(workPatternFields, workSemanticField{"uninterpreted_bytes", "unknown_bytes", '3'})
	workFactorFields = append(workFactorFields, workSemanticField{"uninterpreted_bytes", "unknown_bytes", '4'})
}

type workStatementCache struct {
	mu         sync.Mutex
	statements map[string]*sql.Stmt
}

func (d *DB) workStatement(ctx context.Context, table string, fields []workSemanticField, withHelp bool) (*sql.Stmt, error) {
	d.workStatements.mu.Lock()
	defer d.workStatements.mu.Unlock()
	if stmt := d.workStatements.statements[table]; stmt != nil {
		return stmt, nil
	}
	selects, joins := []string{}, []string{}
	for i, field := range fields {
		if field.kind == 't' {
			alias := fmt.Sprintf("t%d", i)
			selects = append(selects, alias+".value")
			joins = append(joins, "LEFT JOIN text_values "+alias+" ON "+alias+".id=d."+field.column)
		} else {
			selects = append(selects, "d."+field.column)
		}
	}
	if withHelp {
		selects = append(selects, "d.help_definition_id")
	}
	query := "SELECT " + strings.Join(selects, ",") + " FROM " + table + " d " + strings.Join(joins, " ") + " WHERE d.id=?"
	stmt, err := d.db.PrepareContext(ctx, query)
	if err != nil {
		return nil, err
	}
	if d.workStatements.statements == nil {
		d.workStatements.statements = map[string]*sql.Stmt{}
	}
	d.workStatements.statements[table] = stmt
	return stmt, nil
}
func (d *DB) workSemanticRow(ctx context.Context, table string, fields []workSemanticField, id int64, withHelp bool) (map[string]any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid work-factor reference")
	}
	stmt, err := d.workStatement(ctx, table, fields, withHelp)
	if err != nil {
		return nil, err
	}
	count := len(fields)
	if withHelp {
		count++
	}
	values := make([]any, count)
	dest := make([]any, count)
	for i := range dest {
		dest[i] = &values[i]
	}
	if err := stmt.QueryRowContext(ctx, id).Scan(dest...); err != nil {
		return nil, fmt.Errorf("work-factor %s/%d: %w", table, id, err)
	}
	result, err := workDecodedFields(fields, values)
	if err != nil {
		return nil, err
	}
	if withHelp {
		help := []any{}
		if value := values[len(fields)]; value != nil {
			helpID, ok := value.(int64)
			if !ok {
				return nil, fmt.Errorf("invalid work-factor help reference")
			}
			row, err := d.workSemanticRow(ctx, "work_help_definitions", workHelpFields, helpID, false)
			if err != nil {
				return nil, err
			}
			help = append(help, row)
		}
		result["standard_value_help_records"] = help
	}
	return result, nil
}
func (d *DB) cachedWorkSemantic(ctx context.Context, cache *cachedMetadata, table string, fields []workSemanticField, id int64, help bool) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid work-factor reference")
	}
	if value, ok := cache.load(id); ok {
		return value, nil
	}
	value, err := d.workSemanticRow(ctx, table, fields, id, help)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	cache.store(id, value, len(encoded))
	return value, nil
}
func (d *DB) workStandard(ctx context.Context, id int64) (any, error) {
	return d.cachedWorkSemantic(ctx, &d.workStandards, "work_standard_definitions", workStandardFields, id, true)
}
func (d *DB) workPattern(ctx context.Context, id int64) (any, error) {
	return d.cachedWorkSemantic(ctx, &d.workPatterns, "work_pattern_definitions", workPatternFields, id, false)
}
func (d *DB) workFactor(ctx context.Context, id int64) (any, error) {
	return d.cachedWorkSemantic(ctx, &d.workFactors, "work_factor_definitions", workFactorFields, id, false)
}
