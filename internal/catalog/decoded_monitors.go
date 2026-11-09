package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

type decodedMonitorSpec struct {
	integers, texts, ranges, kind, shared string
	children                              bool
}

const decodedOrigin = "record table"
const decodedGates = "exception_handler_flag exception_handler_id"

var decodedMonitorNames = []string{"pattern", "physical", "unit", "pcs_ffd", "data_id", "monitor", "condition", "condition_compact", "dtc_status", "dynamic_did", "behavior", "rob_signal", "rob_special"}
var decodedMonitorSpecs = []decodedMonitorSpec{
	{decodedGates + " key", "source unassigned_fields.14_15 unassigned_fields.18_22 unassigned_fields.8_11", "", "", "pattern", false},
	{"decimal_point_count div " + decodedGates + " key mul offset " + decodedOrigin + " sign_mode unit_key unassigned_fields.23", "source unassigned_fields.18_19", "", "", "", false},
	{decodedGates + " key unassigned_fields.11", "source", "", "", "unit", false},
	{"pid " + decodedOrigin + " request.function_selector request.pid_byte_offset retrieval_group unassigned_word0", "source", "", "pcs_operation_ffd_pid", "", false},
	{"did " + decodedGates + " fixed_length " + decodedOrigin + " support_mode", "source", "", "data_id", "", false},
	{"bit_end bit_start data_range_max data_range_min extraction_mode ffd_support_condition_key flags graph_max graph_min live_support_condition_key monitor_key name_string_index pattern_display_key physical_data_key primary_did " + decodedOrigin + " snapshot_did sort_key visibility_override", "name source unassigned_ranges.10_27 unassigned_ranges.2c_2f unassigned_ranges.44_45 unassigned_ranges.4c_4f", "physical_max physical_min", "monitor", "", true},
	{"bit_end bit_start comparison_mode comparison_value condition_type " + decodedGates + " key " + decodedOrigin + " referenced_did unassigned_tail variable_id", "source variable_bytes", "", "support_condition", "", false},
	{"bit_end bit_start condition_type " + decodedGates + " key " + decodedOrigin + " referenced_did variable_id", "source variable_bytes", "", "support_condition", "", false},
	{"comparison_value " + decodedGates + " key " + decodedOrigin + " status_mask unassigned_fields.byte11 unassigned_fields.word4", "source", "", "dtc_status", "", false},
	{"did " + decodedGates + " maximum_payload_length " + decodedOrigin + " unassigned_tail", "source", "", "dynamic_did", "", false},
	{"behavior_code comment_string_index name_string_index " + decodedOrigin, "comment name signature source unassigned_tail", "", "behavior_code", "", false},
	{"bit_end bit_start did " + decodedGates + " extraction_mode name_string_index pattern_display_key physical_data_key presentation_mode " + decodedOrigin + " record_key sort_key support_condition_key", "name source unassigned_ranges.0_1f unassigned_ranges.24_2b unassigned_ranges.40_41 unassigned_ranges.43_47", "", "rob_signal", "", true},
	{decodedGates + " key " + decodedOrigin + " signal_key unassigned_fields.word6", "source unassigned_fields.tail9", "", "rob_special_signal", "", false},
}
var decodedChildren = [][2]string{{"pattern_records", "pattern"}, {"physical_records", "physical"}, {"unit_records", "unit"}}

func decodedPut(out map[string]any, path string, value any) {
	parts := strings.Split(path, ".")
	for _, part := range parts[:len(parts)-1] {
		child, ok := out[part].(map[string]any)
		if !ok {
			child = map[string]any{}
			out[part] = child
		}
		out = child
	}
	out[parts[len(parts)-1]] = value
}
func (d *DB) decodedMonitor(ctx context.Context, name string, id int64) (any, error) {
	index := decodedDomain(name)
	if index < 0 || id <= 0 {
		return nil, fmt.Errorf("invalid decoded monitor reference")
	}
	if value, ok := d.decodedMonitors[index].load(id); ok {
		return value, nil
	}
	found, err := d.decodedChunk(ctx, index, []int64{id})
	if err != nil {
		return nil, err
	}
	value := found[id]
	encoded, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d.decodedMonitors[index].store(id, value, len(encoded))
	return value, nil
}
