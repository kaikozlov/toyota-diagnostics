package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Only diagnostic references belong in the database; anything else is a
// storage error.
func storedReference(node map[string]any) (string, any, bool) {
	for _, kind := range []string{"$support_member_facts", "$support_query_facts", "$freeze_query_facts", "$support_group_facts", "$customize_item", "$customize_group", "$did_membership", "$dtc_membership", "$display_membership", "$signal_definition", "$data_list_row", "$signal_info", "$did_support", "$support_condition", "$vehicle_definition", "$install_set", "$vehicle_route", "$can_topology", "$diagnostic_code", "$dtc_source_definition", "$freeze_physical", "$operation_base_frame", "$operation_frame", "$operation_encoding", "$operation_mask", "$operation_conversion", "$signal_observation", "$ddr_signal", "$ddr_behavior_signal", "$ddr_condition", "$ddr_behavior_condition", "$ddr_data_id", "$ddr_trigger", "$ddr_behavior_code", "$decoded_pattern", "$decoded_physical", "$decoded_unit", "$decoded_pcs_ffd", "$decoded_data_id", "$decoded_monitor", "$decoded_condition", "$decoded_condition_compact", "$decoded_dtc_status", "$decoded_dynamic_did", "$decoded_behavior", "$decoded_rob_signal", "$decoded_rob_special", "$native_recorder_addresses", "$native_recorder_invalid_conditions", "$native_recorder_monitors", "$native_recorder_triggers", "$native_recorder_case_data_ids", "$native_recorder_header_data_ids", "$legacy_rob_signal", "$legacy_freeze_signal", "$legacy_condition", "$legacy_response_definition", "$legacy_dynamic", "$ecu_catalog", "$work_standard", "$work_pattern", "$work_factor_definition", "$legacy_operation", "$legacy_operation_pattern", "$operation_plan", "$behavior_code"} {
		if id, ok := node[kind]; ok {
			if len(node) != 1 {
				return kind, "non-singleton reference object", true
			}
			return kind, id, true
		}
	}
	// The $ namespace is reserved for storage references. Reject stale or
	// unknown references instead of leaking implementation markers as data.
	for kind, id := range node {
		if strings.HasPrefix(kind, "$") {
			if len(node) != 1 {
				return kind, "non-singleton reference object", true
			}
			return kind, id, true
		}
	}
	return "", nil, false
}
func referenceID(value any, kind string) (int64, error) {
	number, ok := value.(json.Number)
	if !ok {
		return 0, fmt.Errorf("invalid %s reference %v", kind, value)
	}
	id, err := number.Int64()
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("invalid %s reference %v", kind, value)
	}
	return id, nil
}
func catalogNumber(value int64) json.Number { return json.Number(strconv.FormatInt(value, 10)) }
func (d *DB) restoreReference(ctx context.Context, kind string, id int64) (any, error) {
	switch kind {
	case "$support_member_facts", "$support_query_facts", "$freeze_query_facts", "$support_group_facts":
		return d.supportFacts(ctx, kind, id)
	case "$customize_item":
		return d.customizeItem(ctx, id)
	case "$customize_group":
		return d.customizeGroup(ctx, id)
	case "$did_membership", "$dtc_membership", "$display_membership":
		return d.membership(ctx, kind, id)
	case "$signal_definition":
		return d.signalDefinition(ctx, id)
	case "$data_list_row":
		return d.dataListRow(ctx, id)
	case "$signal_info":
		return d.signalInfo(ctx, id)
	case "$did_support":
		return d.didSupport(ctx, id)
	case "$support_condition":
		return d.supportCondition(ctx, id)
	case "$vehicle_definition":
		return d.vehicleDefinition(ctx, id)
	case "$install_set":
		return d.installSet(ctx, id)
	case "$vehicle_route":
		return d.vehicleRoute(ctx, id)
	case "$can_topology":
		return d.canTopology(ctx, id)
	case "$dtc_source_definition":
		return d.dtcSourceDefinition(ctx, id)
	case "$diagnostic_code":
		return d.diagnosticCode(ctx, id)
	case "$freeze_physical":
		return d.freezePhysical(ctx, id)
	case "$signal_observation":
		return d.signalObservation(ctx, id)
	case "$ddr_signal", "$ddr_behavior_signal", "$ddr_condition", "$ddr_behavior_condition", "$ddr_data_id", "$ddr_trigger", "$ddr_behavior_code":
		return d.recorderRecord(ctx, strings.TrimPrefix(kind, "$"), id)
	case "$decoded_pattern", "$decoded_physical", "$decoded_unit", "$decoded_pcs_ffd", "$decoded_data_id", "$decoded_monitor", "$decoded_condition", "$decoded_condition_compact", "$decoded_dtc_status", "$decoded_dynamic_did", "$decoded_behavior", "$decoded_rob_signal", "$decoded_rob_special":
		return d.decodedMonitor(ctx, strings.TrimPrefix(kind, "$decoded_"), id)
	case "$native_recorder_addresses", "$native_recorder_invalid_conditions", "$native_recorder_monitors", "$native_recorder_triggers", "$native_recorder_case_data_ids", "$native_recorder_header_data_ids":
		return d.nativeRecorder(ctx, strings.TrimPrefix(kind, "$native_recorder_"), id)
	case "$legacy_rob_signal":
		return d.legacyROBSignal(ctx, id)
	case "$legacy_freeze_signal":
		return d.legacyFreezeSignal(ctx, id)
	case "$legacy_condition":
		return d.legacyCondition(ctx, id)
	case "$legacy_response_definition":
		return d.legacyResponse(ctx, id)
	case "$legacy_dynamic":
		return d.legacyDynamic(ctx, id)
	case "$ecu_catalog":
		return d.ecuCatalog(ctx, id)
	case "$work_standard":
		return d.workStandard(ctx, id)
	case "$work_pattern":
		return d.workPattern(ctx, id)
	case "$work_factor_definition":
		return d.workFactor(ctx, id)
	case "$legacy_operation":
		return d.legacyOperation(ctx, id)
	case "$behavior_code":
		return d.behaviorCode(ctx, id)
	case "$legacy_operation_pattern":
		return d.legacyOperationPatternView(ctx, id)
	case "$operation_plan":
		return d.operationPlan(ctx, id)
	case "$operation_base_frame", "$operation_frame", "$operation_encoding", "$operation_mask", "$operation_conversion":
		return d.operationDefinition(ctx, kind, id)
	default:
		return nil, fmt.Errorf("unknown diagnostic reference %s", kind)
	}
}
