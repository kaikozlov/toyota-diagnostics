package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

var customizeItemNames = strings.Fields("all_default_gate_u16_22 choice_list_key current_bit_end current_bit_start current_data_mode group_id item_aux_u16_0e item_id legacy_data_id merge_mode metadata_dword_04 name name_string_index raw_u16_1e raw_u16_28 raw_u32_24 raw_u8_33 read_current source_ordinal support_bit_end support_bit_start support_mode support_selector target_category_id target_category_name target_generation target_resolution target_route_selector write_bit_end write_bit_start write_did write_variant")
var customizeGroupNames = strings.Fields("body_type exception_flag exception_id group_id name name_string_index source_ordinal")
var customizeChoiceNames = strings.Fields("exception_flag exception_id name name_string_index source_ordinal value")
var customizeExecutorNames = strings.Fields("family merge read_current session_requirement support_gate verify write")

func customizeFields(names []string, allText bool) []workSemanticField {
	fields := make([]workSemanticField, len(names))
	for i, name := range names {
		if allText || name == "name" || name == "target_category_name" || name == "target_resolution" {
			fields[i] = wt(name, name+"_id")
		} else {
			fields[i] = wf(name)
		}
	}
	return fields
}
func (d *DB) customizeGroup(ctx context.Context, id int64) (any, error) {
	return d.workSemanticRow(ctx, "customize_group_definitions", customizeFields(customizeGroupNames, false), id, false)
}
func (d *DB) customizeItem(ctx context.Context, id int64) (any, error) {
	out, err := d.workSemanticRow(ctx, "customize_item_definitions", customizeFields(customizeItemNames, false), id, false)
	if err != nil {
		return nil, err
	}
	var list int64
	var executor sql.NullInt64
	if err = d.db.QueryRowContext(ctx, "SELECT choice_list_id,executor_id FROM customize_item_definitions WHERE id=?", id).Scan(&list, &executor); err != nil {
		return nil, err
	}
	var count int
	if err = d.db.QueryRowContext(ctx, "SELECT entry_count FROM customize_choice_lists WHERE id=?", list).Scan(&count); err != nil {
		return nil, err
	}
	if count < 0 {
		return nil, fmt.Errorf("invalid customize choice count")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT m.position,c.exception_flag,c.exception_id,c.name_id,t.value,c.name_string_index,c.source_ordinal,c.value
 FROM customize_choice_members m JOIN customize_choice_definitions c ON c.id=m.choice_id LEFT JOIN text_values t ON t.id=c.name_id WHERE m.list_id=? ORDER BY m.position`, list)
	if err != nil {
		return nil, err
	}
	choices := make([]any, 0)
	for rows.Next() {
		var position int
		var flag, exception, index, ordinal, value int64
		var nameID sql.NullInt64
		var name sql.NullString
		if err = rows.Scan(&position, &flag, &exception, &nameID, &name, &index, &ordinal, &value); err != nil {
			rows.Close()
			return nil, err
		}
		if position != len(choices) || nameID.Valid && !name.Valid {
			rows.Close()
			return nil, fmt.Errorf("invalid customize choice relationship")
		}
		var label any
		if name.Valid {
			label = name.String
		}
		num := func(v int64) json.Number { return json.Number(strconv.FormatInt(v, 10)) }
		choices = append(choices, map[string]any{"exception_flag": num(flag), "exception_id": num(exception), "name": label, "name_string_index": num(index), "source_ordinal": num(ordinal), "value": num(value)})
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	if len(choices) != count {
		return nil, fmt.Errorf("incomplete customize choices")
	}
	out["choices"] = choices
	if executor.Valid {
		profile, e := d.workSemanticRow(ctx, "customize_executor_profiles", customizeFields(customizeExecutorNames, true), executor.Int64, false)
		if e != nil {
			return nil, e
		}
		out["modern_executor"] = profile
	}
	return out, nil
}
