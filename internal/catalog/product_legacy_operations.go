package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
)

var legacyOperationNumbers = []string{"active_test_pattern_key", "bit_end", "bit_start", "caution_id", "comment_id", "data_limits_mode", "data_max", "data_max_monitor_key", "data_min", "data_min_monitor_key", "help_id", "id", "info_id", "initial_value", "initial_value_mode", "initial_value_monitor_key", "name_string_index", "pattern_match_count", "physical_data_key", "presentation_pattern", "primary_key", "prohibition_id"}
var legacyPatternNumbers = []string{"auto_continuation_time", "button_size", "key_invalid_flag", "key_operation_pattern", "lock_time", "maintenance_time", "pattern_display_key", "pattern_key", "repeat_enabled", "repeat_interval", "timeout_parameter"}

func (d *DB) legacyOperation(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid legacy operation reference")
	}
	if v, ok := d.legacyOperations.load(id); ok {
		return v, nil
	}
	selected := make([]string, 0, len(legacyOperationNumbers))
	numbers := make([]int64, len(legacyOperationNumbers))
	dest := make([]any, 0, len(numbers)+10)
	for i, k := range legacyOperationNumbers {
		column := k
		if k == "id" {
			column = "operation_id"
		}
		selected = append(selected, "d."+column)
		dest = append(dest, &numbers[i])
	}
	var source, execution, boundary string
	var name sql.NullString
	var unknown sql.NullInt64
	var table, record, program, pattern, signal int64
	var hasName bool
	dest = append(dest, &name, &execution, &boundary, &program, &pattern, &signal, &source, &table, &record, &hasName, &unknown)
	query := "SELECT " + strings.Join(selected, ",") + `,n.value,x.value,b.value,d.program_id,d.pattern_id,d.signal_info_id,s.value,e.table_number,e.record_number,d.name_id IS NOT NULL,e.unknown_id
 FROM legacy_operation_entries e JOIN legacy_operation_defs d ON d.id=e.definition_id
 JOIN text_values s ON s.id=e.source_id LEFT JOIN text_values n ON n.id=d.name_id
 JOIN text_values x ON x.id=d.execution_id JOIN text_values b ON b.id=d.boundary_id
 JOIN signal_infos si ON si.id=d.signal_info_id WHERE e.id=?`
	if err := d.db.QueryRowContext(ctx, query, id).Scan(dest...); err != nil {
		return nil, fmt.Errorf("legacy operation %d: %w", id, err)
	}
	if hasName != name.Valid || signal <= 0 {
		return nil, fmt.Errorf("invalid legacy operation references")
	}
	out := make(map[string]any, len(numbers)+10)
	for i, k := range legacyOperationNumbers {
		out[k] = catalogNumber(numbers[i])
	}
	out["kind"] = "active_test"
	out["name"] = nil
	if name.Valid {
		out["name"] = name.String
	}
	out["execution"] = execution
	out["runtime_boundary"] = boundary
	out["signal_info"] = map[string]any{"$signal_info": catalogNumber(signal)}
	out["source"] = source
	out["table"] = catalogNumber(table)
	out["record"] = catalogNumber(record)
	p, err := d.legacyOperationPattern(ctx, pattern)
	if err != nil {
		return nil, err
	}
	out["pattern"] = p
	programValue, err := d.legacyOperationProgram(ctx, program)
	if err != nil {
		return nil, err
	}
	out["request_program"] = programValue
	if unknown.Valid {
		value, err := d.legacyUnknownFields(ctx, "operation", unknown.Int64)
		if err != nil {
			return nil, err
		}
		out["uninterpreted_bytes"] = value
	}
	raw, err := json.Marshal(out)
	if err != nil {
		return nil, err
	}
	d.legacyOperations.store(id, out, len(raw))
	return out, nil
}
func (d *DB) legacyOperationPattern(ctx context.Context, id int64) (map[string]any, error) {
	selected := make([]string, 0, len(legacyPatternNumbers))
	numbers := make([]int64, len(legacyPatternNumbers))
	dest := make([]any, 0, len(numbers)+3)
	for i, k := range legacyPatternNumbers {
		selected = append(selected, "d."+k)
		dest = append(dest, &numbers[i])
	}
	var source string
	var table, record int64
	var unknown sql.NullInt64
	dest = append(dest, &source, &table, &record, &unknown)
	query := "SELECT " + strings.Join(selected, ",") + `,s.value,e.table_number,e.record_number,e.unknown_id FROM legacy_operation_pattern_entries e
 JOIN legacy_operation_patterns d ON d.id=e.definition_id JOIN text_values s ON s.id=e.source_id WHERE e.id=?`
	if err := d.db.QueryRowContext(ctx, query, id).Scan(dest...); err != nil {
		return nil, fmt.Errorf("legacy operation pattern %d: %w", id, err)
	}
	out := make(map[string]any, len(numbers)+3)
	for i, k := range legacyPatternNumbers {
		out[k] = catalogNumber(numbers[i])
	}
	out["source"] = source
	out["table"] = catalogNumber(table)
	out["record"] = catalogNumber(record)
	if unknown.Valid {
		value, err := d.legacyUnknownFields(ctx, "pattern", unknown.Int64)
		if err != nil {
			return nil, err
		}
		out["uninterpreted_bytes"] = value
	}
	return out, nil
}
func (d *DB) legacyOperationProgram(ctx context.Context, id int64) (map[string]any, error) {
	var encoded, request, mask, support, count, s1, s2, s7, defaultService int64
	var generation, nibble, boundary string
	err := d.db.QueryRowContext(ctx, `SELECT p.encoded,p.request_identifier,p.constant_or_mask,p.support_required,p.step_count,
 g.value,n.value,v.value,s.service1,s.service2,s.service7,s.service_default
 FROM legacy_operation_programs p JOIN legacy_operation_policies s ON s.id=p.policy_id
 JOIN text_values g ON g.id=s.generation7_id JOIN text_values n ON n.id=s.nibble_order_id
 JOIN text_values v ON v.id=s.variant_boundary_id WHERE p.id=?`, id).Scan(&encoded, &request, &mask, &support, &count, &generation, &nibble, &boundary, &s1, &s2, &s7, &defaultService)
	if err != nil {
		return nil, fmt.Errorf("legacy operation program %d: %w", id, err)
	}
	if support < 0 || support > 1 || count < 0 {
		return nil, fmt.Errorf("invalid legacy operation program policy")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT p.position,p.opcode,t.value FROM legacy_operation_steps p JOIN text_values t ON t.id=p.operation_id WHERE p.program_id=? ORDER BY p.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	steps := make([]any, 0)
	for rows.Next() {
		var pos, code int64
		var text string
		if err := rows.Scan(&pos, &code, &text); err != nil {
			return nil, err
		}
		if pos != int64(len(steps)) {
			return nil, fmt.Errorf("invalid legacy operation step order")
		}
		steps = append(steps, map[string]any{"opcode": catalogNumber(code), "operation": text})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if int64(len(steps)) != count {
		return nil, fmt.Errorf("missing legacy operation steps")
	}
	return map[string]any{"encoded": catalogNumber(encoded), "request_identifier": catalogNumber(request), "constant_or_mask": catalogNumber(mask),
		"support_required": support == 1, "generation_7": generation, "nibble_order": nibble, "variant_boundary": boundary,
		"service_by_generation_low5": map[string]any{"1": catalogNumber(s1), "2": catalogNumber(s2), "7": catalogNumber(s7), "default": catalogNumber(defaultService)}, "operations": steps}, nil
}
