-- Schema of catalog/catalog.sqlite. TestFixtureSchemaMatchesShippedCatalog keeps it exact.
CREATE TABLE meta (key TEXT PRIMARY KEY, value TEXT NOT NULL) WITHOUT ROWID;
CREATE TABLE ecus (
  region TEXT NOT NULL, category_id INTEGER NOT NULL, key TEXT NOT NULL,
  name TEXT NOT NULL, database_name TEXT NOT NULL, generation INTEGER,
  support_mode TEXT, catalog_member TEXT, counts TEXT NOT NULL,
  PRIMARY KEY(region, category_id)
) WITHOUT ROWID;
CREATE TABLE search_regions (id INTEGER PRIMARY KEY,name TEXT NOT NULL UNIQUE);
CREATE TABLE search_kinds (id INTEGER PRIMARY KEY,name TEXT NOT NULL UNIQUE);
CREATE TABLE text_values (id INTEGER PRIMARY KEY,value TEXT NOT NULL UNIQUE);
CREATE TABLE search_records (
 id INTEGER PRIMARY KEY,kind_id INTEGER NOT NULL REFERENCES search_kinds(id),
 identifier_id INTEGER NOT NULL REFERENCES text_values(id),
 name_id INTEGER NOT NULL REFERENCES text_values(id),
 detail_id INTEGER NOT NULL REFERENCES text_values(id)
);
CREATE TABLE item_occurrences (
 id INTEGER PRIMARY KEY,region_id INTEGER NOT NULL REFERENCES search_regions(id),
 category_id INTEGER NOT NULL,record_id INTEGER NOT NULL REFERENCES search_records(id),
 document_id INTEGER NOT NULL REFERENCES source_documents(id),
 pointer_id INTEGER NOT NULL REFERENCES source_pointers(id)
);
CREATE TABLE owner_ranges (
 region_id INTEGER NOT NULL REFERENCES search_regions(id),category_id INTEGER NOT NULL,
 first_id INTEGER NOT NULL,last_id INTEGER NOT NULL CHECK(last_id>=first_id),
 PRIMARY KEY(region_id,category_id,first_id)
) WITHOUT ROWID;
CREATE TABLE source_documents (id INTEGER PRIMARY KEY, member TEXT NOT NULL UNIQUE, data BLOB NOT NULL);
CREATE TABLE source_pointers (id INTEGER PRIMARY KEY, pointer TEXT NOT NULL UNIQUE);
CREATE TABLE source_document_parts (
 document_id INTEGER NOT NULL REFERENCES source_documents(id),
 pointer_id INTEGER NOT NULL REFERENCES source_pointers(id),
 data BLOB NOT NULL,
 PRIMARY KEY(document_id,pointer_id)
) WITHOUT ROWID;
CREATE TABLE item_count_rows (
 region_id INTEGER NOT NULL REFERENCES search_regions(id),category_id INTEGER NOT NULL,
 kind_id INTEGER NOT NULL REFERENCES search_kinds(id),count INTEGER NOT NULL,
 PRIMARY KEY(region_id,category_id,kind_id)
) WITHOUT ROWID;
CREATE TABLE diagnostic_codes(
 id INTEGER PRIMARY KEY,code_id INTEGER NOT NULL REFERENCES text_values(id),
 description_id INTEGER REFERENCES text_values(id),comment_id INTEGER REFERENCES text_values(id),
 failure_id INTEGER REFERENCES text_values(id),identifier INTEGER,packed_code INTEGER,
 field_mask INTEGER NOT NULL);
CREATE TABLE dtc_entries(
 id INTEGER PRIMARY KEY,code_id INTEGER NOT NULL REFERENCES diagnostic_codes(id),
 source_id INTEGER REFERENCES text_values(id),table_number INTEGER,record_number INTEGER,
 origin_mask INTEGER NOT NULL);
CREATE TABLE dtc_source_extensions(
 id INTEGER PRIMARY KEY,entry_id INTEGER NOT NULL REFERENCES dtc_entries(id),
 enabled_value INTEGER NOT NULL,description_string_index INTEGER NOT NULL,
 comment_string_index INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,
 exception_handler_flag INTEGER NOT NULL,detail_table INTEGER,detail_lookup_key INTEGER,
 failure_string_index INTEGER,info_byte INTEGER,info_word INTEGER,unknown_bytes BLOB NOT NULL);
CREATE TABLE signal_labels(id INTEGER PRIMARY KEY,monitor_key INTEGER NOT NULL,name_id INTEGER NOT NULL REFERENCES text_values(id),bit_start INTEGER NOT NULL,bit_end INTEGER NOT NULL);
CREATE TABLE signal_pattern_sets(id INTEGER PRIMARY KEY,entry_count INTEGER NOT NULL);
CREATE TABLE signal_pattern_entries(set_id INTEGER NOT NULL REFERENCES signal_pattern_sets(id),key_id INTEGER NOT NULL REFERENCES text_values(id),value_id INTEGER REFERENCES text_values(id),PRIMARY KEY(set_id,key_id)) WITHOUT ROWID;
CREATE TABLE signal_infos(id INTEGER PRIMARY KEY,bit_width INTEGER NOT NULL,decimal_point_count INTEGER NOT NULL,div INTEGER NOT NULL,mul INTEGER NOT NULL,offset INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,sign_mode INTEGER NOT NULL,unit_id INTEGER REFERENCES text_values(id),unit_key INTEGER NOT NULL,pattern_id INTEGER NOT NULL REFERENCES signal_pattern_sets(id),signed_view INTEGER NOT NULL CHECK(signed_view IN(0,1)));
CREATE TABLE signal_did_support(id INTEGER PRIMARY KEY,did INTEGER NOT NULL,fixed_length INTEGER NOT NULL,record INTEGER NOT NULL,encoding_id INTEGER NOT NULL REFERENCES text_values(id),support_mode INTEGER NOT NULL,table_type INTEGER NOT NULL,has_additional INTEGER NOT NULL,additional_count INTEGER NOT NULL);
CREATE TABLE signal_support_additional(parent_id INTEGER NOT NULL REFERENCES signal_did_support(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES signal_did_support(id),PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE signal_conditions(id INTEGER PRIMARY KEY,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,comparison_mode INTEGER NOT NULL,comparison_value INTEGER NOT NULL,condition_type INTEGER NOT NULL,condition_key INTEGER NOT NULL,record INTEGER NOT NULL,referenced_did INTEGER NOT NULL,table_type INTEGER NOT NULL,variable_bytes BLOB NOT NULL,variable_id INTEGER NOT NULL,condition_view INTEGER NOT NULL CHECK(condition_view IN(0,1,2)));
CREATE TABLE signal_monitors(id INTEGER PRIMARY KEY,label_id INTEGER NOT NULL REFERENCES signal_labels(id),alternate_did INTEGER NOT NULL,info_id INTEGER NOT NULL REFERENCES signal_infos(id),support_id INTEGER REFERENCES signal_did_support(id),condition_id INTEGER REFERENCES signal_conditions(id),extraction_mode INTEGER NOT NULL,flags INTEGER NOT NULL,graph_low INTEGER NOT NULL,graph_high INTEGER NOT NULL,list_member INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,physical_low REAL NOT NULL,physical_high REAL NOT NULL,primary_did INTEGER NOT NULL,record INTEGER NOT NULL,sort_key INTEGER NOT NULL,source_id INTEGER NOT NULL REFERENCES text_values(id),table_type INTEGER NOT NULL,visibility_override INTEGER NOT NULL);
CREATE TABLE signal_definitions(id INTEGER PRIMARY KEY,generation INTEGER NOT NULL CHECK(generation IN(5,6)),label_id INTEGER REFERENCES signal_labels(id),alternate_did INTEGER,decimal_point_count INTEGER,decoder_id INTEGER NOT NULL REFERENCES text_values(id),div INTEGER,mul INTEGER,offset INTEGER,unit_id INTEGER REFERENCES text_values(id),graph_low INTEGER,graph_high INTEGER,data_low INTEGER,data_high INTEGER,pattern_id INTEGER REFERENCES signal_pattern_sets(id),signed INTEGER,monitor_id INTEGER REFERENCES signal_monitors(id));
CREATE TABLE data_list_rows(id INTEGER PRIMARY KEY,generation INTEGER NOT NULL CHECK(generation IN(5,6)),label_id INTEGER REFERENCES signal_labels(id),sort_key INTEGER,monitor_id INTEGER REFERENCES signal_monitors(id),did_id INTEGER NOT NULL REFERENCES text_values(id),wire_field INTEGER);
CREATE TABLE signal_observations(
 id INTEGER PRIMARY KEY,shape INTEGER NOT NULL,
 name_id INTEGER REFERENCES text_values(id),decoder_id INTEGER REFERENCES text_values(id),
 info_id INTEGER NOT NULL REFERENCES signal_infos(id),support_id INTEGER REFERENCES signal_did_support(id),
 condition_id INTEGER REFERENCES signal_conditions(id),dynamic_lsb INTEGER,
bit_end INTEGER,
bit_start INTEGER,
local_support_mode INTEGER,
pattern_display_key INTEGER,
physical_data_key INTEGER,
sort_key INTEGER,
did INTEGER,
exception_handler_flag INTEGER,
exception_handler_id INTEGER,
extraction_mode INTEGER,
presentation_mode INTEGER,
record_key INTEGER,
support_condition_key INTEGER,
name_string_index INTEGER,
flags INTEGER,
monitor_key INTEGER,
primary_did INTEGER,
snapshot_did INTEGER,
alternate_did INTEGER,
visibility_override INTEGER,
 physical_low REAL,physical_high REAL,graph_low INTEGER,graph_high INTEGER,auxiliary_tables INTEGER,unknown_bytes BLOB);
CREATE TABLE signal_observation_entries(
 id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES signal_observations(id),
 source_id INTEGER REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE freeze_units(id INTEGER PRIMARY KEY,genre_id INTEGER NOT NULL,record INTEGER NOT NULL,string_index INTEGER NOT NULL,table_type INTEGER NOT NULL,text_id INTEGER REFERENCES text_values(id));
CREATE TABLE freeze_patterns(id INTEGER PRIMARY KEY,record INTEGER NOT NULL,string_index INTEGER NOT NULL,table_type INTEGER NOT NULL,text_id INTEGER REFERENCES text_values(id),value INTEGER NOT NULL);
CREATE TABLE freeze_pattern_lists(id INTEGER PRIMARY KEY,entry_count INTEGER NOT NULL CHECK(entry_count>=0));
CREATE TABLE freeze_pattern_members(list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id),position INTEGER NOT NULL,pattern_id INTEGER NOT NULL REFERENCES freeze_patterns(id),PRIMARY KEY(list_id,position)) WITHOUT ROWID;
CREATE TABLE freeze_physical_defs(id INTEGER PRIMARY KEY,bit_width INTEGER NOT NULL,decimal_point_count INTEGER NOT NULL,div INTEGER NOT NULL,mul INTEGER NOT NULL,offset INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,record INTEGER NOT NULL,sign_mode INTEGER NOT NULL,table_type INTEGER NOT NULL,unit_key INTEGER NOT NULL,unit_id INTEGER NOT NULL REFERENCES freeze_units(id),pattern_list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id));
CREATE TABLE decoded_pattern(id INTEGER PRIMARY KEY,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),unassigned_fields__14_15_id INTEGER REFERENCES text_values(id),unassigned_fields__18_22_id INTEGER REFERENCES text_values(id),unassigned_fields__8_11_id INTEGER REFERENCES text_values(id),value_id INTEGER NOT NULL REFERENCES freeze_patterns(id));
CREATE TABLE decoded_physical(id INTEGER PRIMARY KEY,"decimal_point_count" INTEGER NOT NULL,"div" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"mul" INTEGER NOT NULL,"offset" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"sign_mode" INTEGER NOT NULL,"unit_key" INTEGER NOT NULL,"unassigned_fields__23" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),unassigned_fields__18_19_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_unit(id INTEGER PRIMARY KEY,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"unassigned_fields__11" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),value_id INTEGER NOT NULL REFERENCES freeze_units(id));
CREATE TABLE decoded_pcs_ffd(id INTEGER PRIMARY KEY,"pid" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"request__function_selector" INTEGER NOT NULL,"request__pid_byte_offset" INTEGER NOT NULL,"retrieval_group" INTEGER NOT NULL,"unassigned_word0" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_data_id(id INTEGER PRIMARY KEY,"did" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"fixed_length" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"support_mode" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_monitor(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"data_range_max" INTEGER NOT NULL,"data_range_min" INTEGER NOT NULL,"extraction_mode" INTEGER NOT NULL,"ffd_support_condition_key" INTEGER NOT NULL,"flags" INTEGER NOT NULL,"graph_max" INTEGER NOT NULL,"graph_min" INTEGER NOT NULL,"live_support_condition_key" INTEGER NOT NULL,"monitor_key" INTEGER NOT NULL,"name_string_index" INTEGER NOT NULL,"pattern_display_key" INTEGER NOT NULL,"physical_data_key" INTEGER NOT NULL,"primary_did" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"snapshot_did" INTEGER NOT NULL,"sort_key" INTEGER NOT NULL,"visibility_override" INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),source_id INTEGER REFERENCES text_values(id),unassigned_ranges__10_27_id INTEGER REFERENCES text_values(id),unassigned_ranges__2c_2f_id INTEGER REFERENCES text_values(id),unassigned_ranges__44_45_id INTEGER REFERENCES text_values(id),unassigned_ranges__4c_4f_id INTEGER REFERENCES text_values(id),physical_max REAL NOT NULL,physical_min REAL NOT NULL,pattern_records_count INTEGER NOT NULL CHECK(pattern_records_count>=0),physical_records_count INTEGER NOT NULL CHECK(physical_records_count>=0),unit_records_count INTEGER NOT NULL CHECK(unit_records_count>=0));
CREATE TABLE decoded_monitor_pattern_records(owner_id INTEGER NOT NULL REFERENCES decoded_monitor(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_pattern(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
CREATE TABLE decoded_monitor_physical_records(owner_id INTEGER NOT NULL REFERENCES decoded_monitor(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_physical(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
CREATE TABLE decoded_monitor_unit_records(owner_id INTEGER NOT NULL REFERENCES decoded_monitor(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_unit(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
CREATE TABLE decoded_condition(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"comparison_mode" INTEGER NOT NULL,"comparison_value" INTEGER NOT NULL,"condition_type" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"referenced_did" INTEGER NOT NULL,"unassigned_tail" INTEGER NOT NULL,"variable_id" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),variable_bytes_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_condition_compact(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"condition_type" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"referenced_did" INTEGER NOT NULL,"variable_id" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),variable_bytes_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_dtc_status(id INTEGER PRIMARY KEY,"comparison_value" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"status_mask" INTEGER NOT NULL,"unassigned_fields__byte11" INTEGER NOT NULL,"unassigned_fields__word4" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_dynamic_did(id INTEGER PRIMARY KEY,"did" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"maximum_payload_length" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"unassigned_tail" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_behavior(id INTEGER PRIMARY KEY,"behavior_code" INTEGER NOT NULL,"comment_string_index" INTEGER NOT NULL,"name_string_index" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,comment_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),signature_id INTEGER REFERENCES text_values(id),source_id INTEGER REFERENCES text_values(id),unassigned_tail_id INTEGER REFERENCES text_values(id));
CREATE TABLE decoded_rob_signal(id INTEGER PRIMARY KEY,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"did" INTEGER NOT NULL,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"extraction_mode" INTEGER NOT NULL,"name_string_index" INTEGER NOT NULL,"pattern_display_key" INTEGER NOT NULL,"physical_data_key" INTEGER NOT NULL,"presentation_mode" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"record_key" INTEGER NOT NULL,"sort_key" INTEGER NOT NULL,"support_condition_key" INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),source_id INTEGER REFERENCES text_values(id),unassigned_ranges__0_1f_id INTEGER REFERENCES text_values(id),unassigned_ranges__24_2b_id INTEGER REFERENCES text_values(id),unassigned_ranges__40_41_id INTEGER REFERENCES text_values(id),unassigned_ranges__43_47_id INTEGER REFERENCES text_values(id),pattern_records_count INTEGER NOT NULL CHECK(pattern_records_count>=0),physical_records_count INTEGER NOT NULL CHECK(physical_records_count>=0),unit_records_count INTEGER NOT NULL CHECK(unit_records_count>=0));
CREATE TABLE decoded_rob_signal_pattern_records(owner_id INTEGER NOT NULL REFERENCES decoded_rob_signal(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_pattern(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
CREATE TABLE decoded_rob_signal_physical_records(owner_id INTEGER NOT NULL REFERENCES decoded_rob_signal(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_physical(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
CREATE TABLE decoded_rob_signal_unit_records(owner_id INTEGER NOT NULL REFERENCES decoded_rob_signal(id),position INTEGER NOT NULL,child_id INTEGER NOT NULL REFERENCES decoded_unit(id),PRIMARY KEY(owner_id,position)) WITHOUT ROWID;
CREATE TABLE decoded_rob_special(id INTEGER PRIMARY KEY,"exception_handler_flag" INTEGER NOT NULL,"exception_handler_id" INTEGER NOT NULL,"key" INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,"signal_key" INTEGER NOT NULL,"unassigned_fields__word6" INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),unassigned_fields__tail9_id INTEGER REFERENCES text_values(id));
CREATE TABLE native_recorder_addresses_defs(id INTEGER PRIMARY KEY,address INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,monitor_key INTEGER NOT NULL,record_length INTEGER NOT NULL,section_kind INTEGER NOT NULL,trigger_id INTEGER NOT NULL,header_member INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_13_15_id INTEGER REFERENCES text_values(id),relation_count INTEGER NOT NULL CHECK(relation_count>=0));
CREATE TABLE native_recorder_addresses_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_addresses_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE native_recorder_addresses_links(definition_id INTEGER NOT NULL REFERENCES native_recorder_addresses_defs(id),position INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE native_recorder_invalid_conditions_defs(id INTEGER PRIMARY KEY,combination_mode INTEGER NOT NULL,comparison_mode INTEGER NOT NULL,comparison_value INTEGER NOT NULL,condition_order INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,referenced_signal_key INTEGER NOT NULL,signal_key INTEGER NOT NULL,comparison_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_15_id INTEGER REFERENCES text_values(id));
CREATE TABLE native_recorder_invalid_conditions_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_invalid_conditions_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE native_recorder_monitors_defs(id INTEGER PRIMARY KEY,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,data_max INTEGER NOT NULL,data_min INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,graph_max INTEGER NOT NULL,graph_min INTEGER NOT NULL,history_bit_end INTEGER NOT NULL,history_bit_start INTEGER NOT NULL,monitor_key INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,sort_key INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),signal_info_id INTEGER NOT NULL REFERENCES signal_infos(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_8_11_id INTEGER REFERENCES text_values(id),unknown_42_43_id INTEGER REFERENCES text_values(id),unknown_46_47_id INTEGER REFERENCES text_values(id),relation_count INTEGER NOT NULL CHECK(relation_count>=0));
CREATE TABLE native_recorder_monitors_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_monitors_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE native_recorder_monitors_links(definition_id INTEGER NOT NULL REFERENCES native_recorder_monitors_defs(id),position INTEGER NOT NULL,"record" INTEGER NOT NULL,"table" INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE native_recorder_triggers_defs(id INTEGER PRIMARY KEY,dependent_data_0 INTEGER NOT NULL,dependent_data_1 INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,memo_string_index INTEGER NOT NULL,name_string_index INTEGER NOT NULL,trigger_id INTEGER NOT NULL,trigger_variant INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),memo_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_33_35_id INTEGER REFERENCES text_values(id),relation_count INTEGER NOT NULL CHECK(relation_count>=0));
CREATE TABLE native_recorder_triggers_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_triggers_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE native_recorder_triggers_links(definition_id INTEGER NOT NULL REFERENCES native_recorder_triggers_defs(id),position INTEGER NOT NULL,"value" INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE native_recorder_case_data_ids_defs(id INTEGER PRIMARY KEY,case_did INTEGER NOT NULL,case_parameter INTEGER NOT NULL,case_selector INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,header_did INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_9_id INTEGER REFERENCES text_values(id),unknown_11_id INTEGER REFERENCES text_values(id));
CREATE TABLE native_recorder_case_data_ids_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_case_data_ids_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE native_recorder_header_data_ids_defs(id INTEGER PRIMARY KEY,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,header_did INTEGER NOT NULL,header_parameter INTEGER NOT NULL,family_id INTEGER REFERENCES text_values(id),record_class_id INTEGER REFERENCES text_values(id),uninterpreted_present INTEGER NOT NULL CHECK(uninterpreted_present IN(0,1)),unknown_7_id INTEGER REFERENCES text_values(id));
CREATE TABLE native_recorder_header_data_ids_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES native_recorder_header_data_ids_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE legacy_condition_defs(
 id INTEGER PRIMARY KEY,condition_key INTEGER NOT NULL,variable_id INTEGER NOT NULL,
 bit_start INTEGER NOT NULL,bit_end INTEGER NOT NULL,source_mode INTEGER NOT NULL,
 exception_id INTEGER NOT NULL,exception_flag INTEGER NOT NULL,variable BLOB);
CREATE TABLE legacy_condition_entries(
 id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_condition_defs(id),
 source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,
 record_number INTEGER NOT NULL,has_kind INTEGER NOT NULL CHECK(has_kind IN(0,1)));
CREATE TABLE legacy_response_defs(
 id INTEGER PRIMARY KEY,response_key INTEGER NOT NULL,data_length INTEGER NOT NULL,
 exception_id INTEGER NOT NULL,exception_flag INTEGER NOT NULL);
CREATE TABLE legacy_response_entries(
 id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_response_defs(id),
 source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE legacy_dynamic_defs(
 id INTEGER PRIMARY KEY,monitor_key INTEGER NOT NULL,mul INTEGER NOT NULL,div INTEGER NOT NULL,
 bit_start INTEGER NOT NULL,bit_end INTEGER NOT NULL,pid INTEGER NOT NULL);
CREATE TABLE legacy_dynamic_entries(
 id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_dynamic_defs(id),
 record_number INTEGER NOT NULL,unknown_id INTEGER REFERENCES legacy_dynamic_unknown_facts(id));
CREATE TABLE legacy_operation_unknown_facts(id INTEGER PRIMARY KEY,range_0_19_id INTEGER NOT NULL REFERENCES text_values(id),range_60_61_id INTEGER NOT NULL REFERENCES text_values(id),range_70_73_id INTEGER NOT NULL REFERENCES text_values(id),range_80_81_id INTEGER NOT NULL REFERENCES text_values(id),range_83_id INTEGER NOT NULL REFERENCES text_values(id),range_86_87_id INTEGER NOT NULL REFERENCES text_values(id),range_90_91_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE legacy_pattern_unknown_facts(id INTEGER PRIMARY KEY,range_14_17_id INTEGER NOT NULL REFERENCES text_values(id),range_22_23_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE legacy_dynamic_unknown_facts(id INTEGER PRIMARY KEY,range_14_16_id INTEGER NOT NULL REFERENCES text_values(id),range_18_end_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE legacy_freeze_definitions(
 id INTEGER PRIMARY KEY,name_id INTEGER REFERENCES text_values(id),
 short_name_id INTEGER NOT NULL REFERENCES text_values(id),layout_id INTEGER NOT NULL REFERENCES text_values(id),
 physical_id INTEGER NOT NULL REFERENCES freeze_physical_defs(id),
begin_condition INTEGER NOT NULL,
bit_end INTEGER NOT NULL,
bit_start INTEGER NOT NULL,
data_type INTEGER NOT NULL,
exception_handler_flag INTEGER NOT NULL,
exception_handler_id INTEGER NOT NULL,
graph_max INTEGER NOT NULL,
graph_min INTEGER NOT NULL,
membership_flags INTEGER NOT NULL,
monitor_key INTEGER NOT NULL,
name_string_index INTEGER NOT NULL,
pattern_display_key INTEGER NOT NULL,
physical_data_key INTEGER NOT NULL,
pid INTEGER NOT NULL,
slope_condition INTEGER NOT NULL,
slope_default INTEGER NOT NULL,
sort_key INTEGER NOT NULL,
support_condition_key INTEGER NOT NULL,
support_policy INTEGER NOT NULL,
trigger_condition INTEGER NOT NULL,
freeze_member INTEGER NOT NULL,
live_member INTEGER NOT NULL,
support_condition_resolved INTEGER NOT NULL,
 data_min REAL NOT NULL,data_max REAL NOT NULL,support_count INTEGER NOT NULL,dynamic_count INTEGER NOT NULL);
CREATE TABLE legacy_freeze_unknown_facts(
 id INTEGER PRIMARY KEY,bytes_34_3f BLOB NOT NULL,bytes_48_4b BLOB NOT NULL,
 bytes_56_57 BLOB NOT NULL,bytes_5e_5f BLOB NOT NULL,byte_67 INTEGER NOT NULL);
CREATE TABLE legacy_freeze_entries(
 id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_freeze_definitions(id),
 source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL,
 unknown_id INTEGER REFERENCES legacy_freeze_unknown_facts(id));
CREATE TABLE legacy_freeze_support_members(
 definition_id INTEGER NOT NULL REFERENCES legacy_freeze_definitions(id),position INTEGER NOT NULL,
 condition_id INTEGER NOT NULL REFERENCES legacy_condition_entries(id),PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE legacy_freeze_dynamic_members(
 definition_id INTEGER NOT NULL REFERENCES legacy_freeze_definitions(id),position INTEGER NOT NULL,
 dynamic_id INTEGER NOT NULL REFERENCES legacy_dynamic_entries(id),PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE legacy_operation_policies(id INTEGER PRIMARY KEY,generation7_id INTEGER NOT NULL REFERENCES text_values(id),nibble_order_id INTEGER NOT NULL REFERENCES text_values(id),variant_boundary_id INTEGER NOT NULL REFERENCES text_values(id),service1 INTEGER NOT NULL,service2 INTEGER NOT NULL,service7 INTEGER NOT NULL,service_default INTEGER NOT NULL);
CREATE TABLE legacy_operation_programs(id INTEGER PRIMARY KEY,encoded INTEGER NOT NULL,request_identifier INTEGER NOT NULL,constant_or_mask INTEGER NOT NULL,support_required INTEGER NOT NULL CHECK(support_required IN(0,1)),policy_id INTEGER NOT NULL REFERENCES legacy_operation_policies(id),step_count INTEGER NOT NULL CHECK(step_count>=0));
CREATE TABLE legacy_operation_steps(program_id INTEGER NOT NULL REFERENCES legacy_operation_programs(id),position INTEGER NOT NULL,opcode INTEGER NOT NULL,operation_id INTEGER NOT NULL REFERENCES text_values(id),PRIMARY KEY(program_id,position)) WITHOUT ROWID;
CREATE TABLE legacy_operation_patterns(id INTEGER PRIMARY KEY,auto_continuation_time INTEGER NOT NULL,button_size INTEGER NOT NULL,key_invalid_flag INTEGER NOT NULL,key_operation_pattern INTEGER NOT NULL,lock_time INTEGER NOT NULL,maintenance_time INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,pattern_key INTEGER NOT NULL,repeat_enabled INTEGER NOT NULL,repeat_interval INTEGER NOT NULL,timeout_parameter INTEGER NOT NULL);
CREATE TABLE legacy_operation_pattern_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_operation_patterns(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL,unknown_id INTEGER REFERENCES legacy_pattern_unknown_facts(id));
CREATE TABLE legacy_operation_defs(id INTEGER PRIMARY KEY,"active_test_pattern_key" INTEGER NOT NULL,"bit_end" INTEGER NOT NULL,"bit_start" INTEGER NOT NULL,"caution_id" INTEGER NOT NULL,"comment_id" INTEGER NOT NULL,"data_limits_mode" INTEGER NOT NULL,"data_max" INTEGER NOT NULL,"data_max_monitor_key" INTEGER NOT NULL,"data_min" INTEGER NOT NULL,"data_min_monitor_key" INTEGER NOT NULL,"help_id" INTEGER NOT NULL,"operation_id" INTEGER NOT NULL,"info_id" INTEGER NOT NULL,"initial_value" INTEGER NOT NULL,"initial_value_mode" INTEGER NOT NULL,"initial_value_monitor_key" INTEGER NOT NULL,"name_string_index" INTEGER NOT NULL,"pattern_match_count" INTEGER NOT NULL,"physical_data_key" INTEGER NOT NULL,"presentation_pattern" INTEGER NOT NULL,"primary_key" INTEGER NOT NULL,"prohibition_id" INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),execution_id INTEGER NOT NULL REFERENCES text_values(id),boundary_id INTEGER NOT NULL REFERENCES text_values(id),program_id INTEGER NOT NULL REFERENCES legacy_operation_programs(id),pattern_id INTEGER NOT NULL REFERENCES legacy_operation_pattern_entries(id),signal_info_id INTEGER NOT NULL REFERENCES signal_infos(id));
CREATE TABLE legacy_operation_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_operation_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL,unknown_id INTEGER REFERENCES legacy_operation_unknown_facts(id));
CREATE TABLE legacy_rob_physical(id INTEGER PRIMARY KEY,
decimal_point_count INTEGER NOT NULL,div INTEGER NOT NULL,mul INTEGER NOT NULL,offset INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,record INTEGER NOT NULL,sign_mode INTEGER NOT NULL,table_number INTEGER NOT NULL,unit_key INTEGER NOT NULL,unit_id INTEGER NOT NULL REFERENCES freeze_units(id));
CREATE TABLE legacy_rob_definitions(id INTEGER PRIMARY KEY,
bdid INTEGER NOT NULL,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,graph_max INTEGER NOT NULL,graph_min INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,signal_key INTEGER NOT NULL,sort_key INTEGER NOT NULL,support_mode INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),short_name_id INTEGER REFERENCES text_values(id),decoder_id INTEGER REFERENCES text_values(id),support_rule_id INTEGER REFERENCES text_values(id)
,data_min REAL NOT NULL,data_max REAL NOT NULL,pattern_list_id INTEGER NOT NULL REFERENCES freeze_pattern_lists(id),physical_count INTEGER NOT NULL);
CREATE TABLE legacy_rob_variants(definition_id INTEGER NOT NULL REFERENCES legacy_rob_definitions(id),position INTEGER NOT NULL,physical_id INTEGER NOT NULL REFERENCES legacy_rob_physical(id),PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE legacy_rob_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES legacy_rob_definitions(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE ddr_signal_defs(id INTEGER PRIMARY KEY,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,did INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,signal_key INTEGER NOT NULL,signal_role INTEGER NOT NULL,sort_key INTEGER NOT NULL,visibility INTEGER NOT NULL,list_member INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),data_min REAL NOT NULL,data_max REAL NOT NULL,signal_info_id INTEGER NOT NULL REFERENCES signal_infos(id),data_id_records_count INTEGER NOT NULL,invalid_condition_records_count INTEGER NOT NULL);
CREATE TABLE ddr_signal_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_signal_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE ddr_signal_data_id_records(definition_id INTEGER NOT NULL REFERENCES ddr_signal_defs(id),position INTEGER NOT NULL,value INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE ddr_signal_invalid_condition_records(definition_id INTEGER NOT NULL REFERENCES ddr_signal_defs(id),position INTEGER NOT NULL,value INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE ddr_behavior_signal_defs(id INTEGER PRIMARY KEY,bit_end INTEGER NOT NULL,bit_start INTEGER NOT NULL,did INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,pattern_display_key INTEGER NOT NULL,physical_data_key INTEGER NOT NULL,presentation_mode INTEGER NOT NULL,signal_key INTEGER NOT NULL,signal_role INTEGER NOT NULL,sort_key INTEGER NOT NULL,extraction_mode INTEGER NOT NULL,graph_max INTEGER NOT NULL,graph_min INTEGER NOT NULL,support_condition_key INTEGER NOT NULL,live_list_member INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),data_min REAL NOT NULL,data_max REAL NOT NULL,signal_info_id INTEGER NOT NULL REFERENCES signal_infos(id),invalid_condition_records_count INTEGER NOT NULL);
CREATE TABLE ddr_behavior_signal_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_behavior_signal_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE ddr_behavior_signal_invalid_condition_records(definition_id INTEGER NOT NULL REFERENCES ddr_behavior_signal_defs(id),position INTEGER NOT NULL,value INTEGER NOT NULL,PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE ddr_condition_defs(id INTEGER PRIMARY KEY,combination_mode INTEGER NOT NULL,comparison_mode INTEGER NOT NULL,comparison_value INTEGER NOT NULL,condition_order INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,referenced_signal_key INTEGER NOT NULL,signal_key INTEGER NOT NULL,comparison_id INTEGER REFERENCES text_values(id),unknown_byte15 TEXT CHECK(unknown_byte15 IS NULL OR (length(unknown_byte15)=2 AND unknown_byte15 NOT GLOB '*[^0-9a-f]*')));
CREATE TABLE ddr_condition_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_condition_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE ddr_behavior_condition_defs(id INTEGER PRIMARY KEY,combination_mode INTEGER NOT NULL,comparison_mode INTEGER NOT NULL,condition_order INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,referenced_signal_key INTEGER NOT NULL,signal_key INTEGER NOT NULL,comparison_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),comparison_bits64 TEXT NOT NULL);
CREATE TABLE ddr_behavior_condition_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_behavior_condition_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE ddr_data_id_defs(id INTEGER PRIMARY KEY,did INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,payload_length INTEGER NOT NULL,validity_mode INTEGER NOT NULL);
CREATE TABLE ddr_data_id_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_data_id_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE ddr_trigger_defs(id INTEGER PRIMARY KEY,dtc_identifier INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,trigger_memo_string_index INTEGER NOT NULL,trigger_point_enabled INTEGER NOT NULL,trigger_selector_0 INTEGER NOT NULL,trigger_selector_1 INTEGER NOT NULL,unresolved_u16_0e INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),trigger_memo_id INTEGER REFERENCES text_values(id));
CREATE TABLE ddr_trigger_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_trigger_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE ddr_behavior_code_defs(id INTEGER PRIMARY KEY,additional_string_index INTEGER NOT NULL,behavior_key INTEGER NOT NULL,dependent_data_0 INTEGER NOT NULL,dependent_data_1 INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_string_index INTEGER NOT NULL,trigger_memo_string_index INTEGER NOT NULL,trigger_selector_0 INTEGER NOT NULL,trigger_selector_1 INTEGER NOT NULL,additional_text_id INTEGER REFERENCES text_values(id),family_id INTEGER REFERENCES text_values(id),name_id INTEGER REFERENCES text_values(id),trigger_memo_id INTEGER REFERENCES text_values(id));
CREATE TABLE ddr_behavior_code_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES ddr_behavior_code_defs(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE behavior_code_defs(
 id INTEGER PRIMARY KEY,variant INTEGER NOT NULL CHECK(variant IN(0,1)),
 behavior_code INTEGER NOT NULL,comment_string_index INTEGER,division_string_index INTEGER,
 enabled INTEGER,exception_handler_flag INTEGER,exception_handler_id INTEGER,memory_override INTEGER,name_string_index INTEGER,
 name_id INTEGER REFERENCES text_values(id),comment_id INTEGER REFERENCES text_values(id),
 signature_id INTEGER NOT NULL REFERENCES text_values(id),division_id INTEGER REFERENCES text_values(id),kind_id INTEGER REFERENCES text_values(id),unknown_bytes BLOB);
CREATE TABLE behavior_code_entries(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES behavior_code_defs(id),
 source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE freeze_query_policies(id INTEGER PRIMARY KEY,length3_offset INTEGER NOT NULL,length4_offset INTEGER NOT NULL);
CREATE TABLE support_member_facts(id INTEGER PRIMARY KEY,identifier INTEGER NOT NULL,list_key INTEGER NOT NULL,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL);
CREATE TABLE support_member_origins(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES support_member_facts(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE support_query_facts(id INTEGER PRIMARY KEY,query_id INTEGER NOT NULL,policy INTEGER NOT NULL,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,kind_id INTEGER NOT NULL REFERENCES text_values(id),requires_previous INTEGER NOT NULL CHECK(requires_previous IN(0,1)));
CREATE TABLE support_query_origins(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES support_query_facts(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE support_freeze_facts(id INTEGER PRIMARY KEY,freeze_frame_number INTEGER NOT NULL,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,policy_id INTEGER NOT NULL REFERENCES freeze_query_policies(id));
CREATE TABLE support_freeze_origins(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES support_freeze_facts(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE support_group_facts(id INTEGER PRIMARY KEY,group_id INTEGER NOT NULL,list_key INTEGER NOT NULL,member_table INTEGER NOT NULL,name_string_index INTEGER NOT NULL,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,name_id INTEGER NOT NULL REFERENCES text_values(id),member_count INTEGER NOT NULL CHECK(member_count>=0));
CREATE TABLE support_group_origins(id INTEGER PRIMARY KEY,definition_id INTEGER NOT NULL REFERENCES support_group_facts(id),source_id INTEGER NOT NULL REFERENCES text_values(id),table_number INTEGER NOT NULL,record_number INTEGER NOT NULL);
CREATE TABLE support_group_links(definition_id INTEGER NOT NULL REFERENCES support_group_facts(id),position INTEGER NOT NULL,member_id INTEGER NOT NULL REFERENCES support_member_origins(id),PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE work_help_definitions(id INTEGER PRIMARY KEY,help_id INTEGER NOT NULL,kind INTEGER NOT NULL,namespace_id INTEGER REFERENCES text_values(id),record INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),string_index INTEGER NOT NULL,string_source_id INTEGER REFERENCES text_values(id),table_type INTEGER NOT NULL,text_id INTEGER REFERENCES text_values(id),translation_available INTEGER NOT NULL CHECK(translation_available IN(0,1)),unknown_bytes BLOB);
CREATE TABLE work_standard_definitions(id INTEGER PRIMARY KEY,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,help_id INTEGER NOT NULL,record INTEGER NOT NULL,signal_id INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),standard_value_id_text INTEGER REFERENCES text_values(id),standard_value_id INTEGER NOT NULL,standard_value_status_id INTEGER REFERENCES text_values(id),table_type INTEGER NOT NULL,help_definition_id INTEGER REFERENCES work_help_definitions(id),unknown_bytes BLOB);
CREATE TABLE work_pattern_definitions(id INTEGER PRIMARY KEY,display_code INTEGER NOT NULL,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,help_id_text INTEGER REFERENCES text_values(id),help_string_index INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),name_string_index INTEGER NOT NULL,pattern_id INTEGER NOT NULL,record INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),table_type INTEGER NOT NULL,value INTEGER NOT NULL,unknown_bytes BLOB);
CREATE TABLE work_factor_definitions(id INTEGER PRIMARY KEY,exception_handler_flag INTEGER NOT NULL,exception_handler_id INTEGER NOT NULL,help_id_text INTEGER REFERENCES text_values(id),help_string_index INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),name_string_index INTEGER NOT NULL,record INTEGER NOT NULL,signal_id INTEGER NOT NULL,source_id INTEGER REFERENCES text_values(id),table_type INTEGER NOT NULL,value INTEGER NOT NULL,unknown_bytes BLOB);
CREATE TABLE operation_wire_frames(id INTEGER PRIMARY KEY,selector_id INTEGER NOT NULL REFERENCES text_values(id),comm_set INTEGER NOT NULL,send BLOB NOT NULL,mask BLOB NOT NULL,check_bytes BLOB NOT NULL,receive_timeout INTEGER NOT NULL,retry_count INTEGER NOT NULL);
CREATE TABLE operation_base_frames(id INTEGER PRIMARY KEY,wire_id INTEGER NOT NULL REFERENCES operation_wire_frames(id),category_id INTEGER NOT NULL,frame_id INTEGER NOT NULL REFERENCES text_values(id),send_id INTEGER NOT NULL REFERENCES text_values(id),send_normalized_id INTEGER NOT NULL REFERENCES text_values(id),mask_id INTEGER NOT NULL REFERENCES text_values(id),mask_normalized_id INTEGER NOT NULL REFERENCES text_values(id),check_id INTEGER NOT NULL REFERENCES text_values(id),check_normalized_id INTEGER NOT NULL REFERENCES text_values(id),send_parameter INTEGER NOT NULL,unknown_word INTEGER NOT NULL,exception_id INTEGER NOT NULL,exception_flag INTEGER NOT NULL);
CREATE TABLE operation_frames(id INTEGER PRIMARY KEY,wire_id INTEGER NOT NULL REFERENCES operation_wire_frames(id),frame_id INTEGER NOT NULL REFERENCES text_values(id),name_id INTEGER NOT NULL REFERENCES text_values(id),session_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE operation_encoding_policies(id INTEGER PRIMARY KEY,p5_supported INTEGER NOT NULL,p6_supported INTEGER NOT NULL,unsupported_status_id INTEGER NOT NULL REFERENCES text_values(id),source_scope_id INTEGER NOT NULL REFERENCES text_values(id),value_input_id INTEGER NOT NULL REFERENCES text_values(id),adapter_id INTEGER NOT NULL REFERENCES text_values(id),engine_selection_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE operation_mask_policies(id INTEGER PRIMARY KEY,start_id INTEGER NOT NULL REFERENCES text_values(id),stop_id INTEGER NOT NULL REFERENCES text_values(id),rule_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE operation_conversions(id INTEGER PRIMARY KEY,formula_id INTEGER NOT NULL REFERENCES text_values(id),source_id INTEGER NOT NULL REFERENCES text_values(id),value_integer_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE plan_variable(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_bytes BLOB NOT NULL,f_id INTEGER REFERENCES text_values(id) NOT NULL,f_normalized_id INTEGER REFERENCES text_values(id) NOT NULL);
CREATE TABLE plan_formula_frame(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_base_frame INTEGER REFERENCES operation_base_frames(id) NOT NULL,f_formula INTEGER REFERENCES text_values(id) NOT NULL,f_materialized_prefix BLOB NOT NULL);
CREATE TABLE plan_static_frame(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_base_frame INTEGER REFERENCES operation_base_frames(id) NOT NULL,f_materialized_static_request BLOB NOT NULL,f_static_command_variable INTEGER REFERENCES plan_variable(id));
CREATE TABLE plan_probe_frame(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_base_frame INTEGER REFERENCES operation_base_frames(id) NOT NULL,f_materialized_request BLOB NOT NULL,f_positive_check BLOB NOT NULL);
CREATE TABLE plan_length_probe(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_decoded_value_formula INTEGER REFERENCES text_values(id) NOT NULL,f_kind INTEGER REFERENCES text_values(id) NOT NULL,f_received_length_formula INTEGER REFERENCES text_values(id) NOT NULL,f_selector INTEGER REFERENCES text_values(id) NOT NULL,f_response_prefix_length INTEGER NOT NULL,f_frames INTEGER NOT NULL CHECK(f_frames>=0));
CREATE TABLE plan_length_probe_frames(parent_id INTEGER NOT NULL REFERENCES plan_length_probe(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_probe_frame(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_initial_read(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_mode INTEGER NOT NULL,f_frames INTEGER CHECK(f_frames>=0),f_name INTEGER REFERENCES text_values(id),f_resolved INTEGER,f_selector INTEGER REFERENCES text_values(id));
CREATE TABLE plan_initial_read_frames(parent_id INTEGER NOT NULL REFERENCES plan_initial_read(id),position INTEGER NOT NULL,value INTEGER REFERENCES operation_frames(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_example_start(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_raw_0 BLOB NOT NULL,f_raw_1 BLOB NOT NULL);
CREATE TABLE plan_example_stop(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_return_control BLOB NOT NULL);
CREATE TABLE plan_examples(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_qualification INTEGER REFERENCES text_values(id) NOT NULL,f_start INTEGER NOT NULL CHECK(f_start>=0),f_stop INTEGER NOT NULL CHECK(f_stop>=0));
CREATE TABLE plan_examples_start(parent_id INTEGER NOT NULL REFERENCES plan_examples(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_example_start(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_examples_stop(parent_id INTEGER NOT NULL REFERENCES plan_examples(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_example_stop(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_data_binding(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_control_enable_bit_1based INTEGER NOT NULL,f_data_byte_length INTEGER NOT NULL,f_data_byte_offset INTEGER NOT NULL,f_did INTEGER NOT NULL,f_encoding_mode INTEGER NOT NULL,f_mode5_bit_end_match INTEGER NOT NULL,f_mode5_bit_start_match INTEGER NOT NULL,f_record INTEGER NOT NULL);
CREATE TABLE plan_choice(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_text INTEGER REFERENCES text_values(id) NOT NULL,f_value INTEGER NOT NULL);
CREATE TABLE plan_presentation_pattern(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_auto_continue_time INTEGER NOT NULL,f_button_size INTEGER NOT NULL,f_key INTEGER NOT NULL,f_key_invalid_flag INTEGER NOT NULL,f_key_operation_pattern INTEGER NOT NULL,f_lock_time INTEGER NOT NULL,f_maintenance_time INTEGER NOT NULL,f_pattern_display_key INTEGER NOT NULL);
CREATE TABLE plan_scaling(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_decimal_point_count INTEGER NOT NULL,f_div INTEGER NOT NULL,f_key INTEGER NOT NULL,f_mul INTEGER NOT NULL,f_offset INTEGER NOT NULL,f_unit_genre_id INTEGER NOT NULL,f_unit_key INTEGER NOT NULL,f_signed INTEGER NOT NULL,f_unit INTEGER REFERENCES text_values(id));
CREATE TABLE plan_signal(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_choices INTEGER NOT NULL CHECK(f_choices>=0),f_engineering_to_raw INTEGER REFERENCES operation_conversions(id) NOT NULL,f_pattern INTEGER REFERENCES plan_presentation_pattern(id) NOT NULL,f_physical INTEGER REFERENCES plan_scaling(id) NOT NULL);
CREATE TABLE plan_signal_choices(parent_id INTEGER NOT NULL REFERENCES plan_signal(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_choice(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_support_gate(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_family INTEGER REFERENCES text_values(id) NOT NULL,f_inventory INTEGER REFERENCES text_values(id) NOT NULL,f_kind INTEGER REFERENCES text_values(id) NOT NULL,f_mode INTEGER REFERENCES text_values(id) NOT NULL,f_identifier INTEGER NOT NULL,f_length_probe INTEGER REFERENCES text_values(id));
CREATE TABLE plan_direct(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_execution INTEGER REFERENCES text_values(id) NOT NULL,f_kind INTEGER REFERENCES text_values(id) NOT NULL,f_name INTEGER REFERENCES text_values(id) NOT NULL,f_id INTEGER NOT NULL,f_bit_end INTEGER NOT NULL,f_bit_start INTEGER NOT NULL,f_did INTEGER NOT NULL,f_encoding_mode INTEGER NOT NULL,f_positive_response INTEGER NOT NULL,f_runtime_length_minimum INTEGER NOT NULL,f_service INTEGER NOT NULL,f_monitor_name INTEGER REFERENCES text_values(id) NOT NULL,f_session_requirement INTEGER REFERENCES text_values(id) NOT NULL,f_monitor_key INTEGER,f_control_enable_mask INTEGER REFERENCES operation_mask_policies(id) NOT NULL,f_encoding_requirements INTEGER REFERENCES operation_encoding_policies(id) NOT NULL,f_data_id_for_act_records INTEGER NOT NULL CHECK(f_data_id_for_act_records>=0),f_initial_read INTEGER REFERENCES plan_initial_read(id) NOT NULL,f_minimum_examples INTEGER REFERENCES plan_examples(id),f_runtime_length_probe INTEGER REFERENCES plan_length_probe(id) NOT NULL,f_signal_info INTEGER REFERENCES plan_signal(id) NOT NULL,f_start_frames INTEGER NOT NULL CHECK(f_start_frames>=0),f_stop_frames INTEGER NOT NULL CHECK(f_stop_frames>=0),f_support_gate INTEGER REFERENCES plan_support_gate(id),f_multi_control_group INTEGER,f_reason INTEGER REFERENCES text_values(id));
CREATE TABLE plan_direct_data_id_for_act_records(parent_id INTEGER NOT NULL REFERENCES plan_direct(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_data_binding(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_direct_start_frames(parent_id INTEGER NOT NULL REFERENCES plan_direct(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_formula_frame(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_direct_stop_frames(parent_id INTEGER NOT NULL REFERENCES plan_direct(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_formula_frame(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_routine(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_execution INTEGER REFERENCES text_values(id) NOT NULL,f_kind INTEGER REFERENCES text_values(id) NOT NULL,f_name INTEGER REFERENCES text_values(id) NOT NULL,f_id INTEGER NOT NULL,f_output_mask_button_variable INTEGER NOT NULL,f_output_mask_value_variable INTEGER NOT NULL,f_positive_response INTEGER NOT NULL,f_routine_command_variable INTEGER NOT NULL,f_routine_id INTEGER NOT NULL,f_routine_status_key INTEGER NOT NULL,f_routine_stop_command_variable INTEGER NOT NULL,f_service INTEGER NOT NULL,f_fixed_request INTEGER NOT NULL,f_session_requirement INTEGER REFERENCES text_values(id) NOT NULL,f_output_mask_button INTEGER REFERENCES plan_variable(id) NOT NULL,f_output_mask_value INTEGER REFERENCES plan_variable(id) NOT NULL,f_routine_commands INTEGER NOT NULL CHECK(f_routine_commands>=0),f_routine_stop_commands INTEGER NOT NULL CHECK(f_routine_stop_commands>=0),f_start_frames INTEGER NOT NULL CHECK(f_start_frames>=0),f_stop_frames INTEGER NOT NULL CHECK(f_stop_frames>=0),f_result_frames INTEGER NOT NULL CHECK(f_result_frames>=0),f_support_gate INTEGER REFERENCES plan_support_gate(id));
CREATE TABLE plan_routine_routine_commands(parent_id INTEGER NOT NULL REFERENCES plan_routine(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_variable(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_routine_routine_stop_commands(parent_id INTEGER NOT NULL REFERENCES plan_routine(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_variable(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_routine_start_frames(parent_id INTEGER NOT NULL REFERENCES plan_routine(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_static_frame(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_routine_stop_frames(parent_id INTEGER NOT NULL REFERENCES plan_routine(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_static_frame(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_routine_result_frames(parent_id INTEGER NOT NULL REFERENCES plan_routine(id),position INTEGER NOT NULL,value INTEGER REFERENCES plan_static_frame(id) NOT NULL,PRIMARY KEY(parent_id,position)) WITHOUT ROWID;
CREATE TABLE plan_refused(id INTEGER PRIMARY KEY,present INTEGER NOT NULL,f_execution INTEGER REFERENCES text_values(id) NOT NULL,f_kind INTEGER REFERENCES text_values(id) NOT NULL,f_name INTEGER REFERENCES text_values(id) NOT NULL,f_id INTEGER NOT NULL,f_record INTEGER NOT NULL,f_routine_id INTEGER,f_error INTEGER REFERENCES text_values(id) NOT NULL);
CREATE TABLE operation_plan_entries(id INTEGER PRIMARY KEY,direct_id INTEGER REFERENCES plan_direct(id),routine_id INTEGER REFERENCES plan_routine(id),refused_id INTEGER REFERENCES plan_refused(id),CHECK((direct_id IS NOT NULL)+(routine_id IS NOT NULL)+(refused_id IS NOT NULL)=1));
CREATE TABLE did_membership_sets(
 id INTEGER PRIMARY KEY,group_count INTEGER NOT NULL CHECK(group_count>=0));
CREATE TABLE did_membership_groups(
 set_id INTEGER NOT NULL REFERENCES did_membership_sets(id),
 position INTEGER NOT NULL CHECK(position>=0),key TEXT NOT NULL,
 member_count INTEGER NOT NULL CHECK(member_count>=0),
 PRIMARY KEY(set_id,position),UNIQUE(set_id,key)) WITHOUT ROWID;
CREATE TABLE did_membership_rows(
 set_id INTEGER NOT NULL,group_position INTEGER NOT NULL,position INTEGER NOT NULL CHECK(position>=0),
 definition_id INTEGER NOT NULL REFERENCES signal_definitions(id),
 PRIMARY KEY(set_id,group_position,position),
 FOREIGN KEY(set_id,group_position) REFERENCES did_membership_groups(set_id,position)) WITHOUT ROWID;
CREATE TABLE dtc_membership_sets(
 id INTEGER PRIMARY KEY,group_count INTEGER NOT NULL CHECK(group_count>=0));
CREATE TABLE dtc_membership_groups(
 set_id INTEGER NOT NULL REFERENCES dtc_membership_sets(id),
 position INTEGER NOT NULL CHECK(position>=0),key TEXT NOT NULL,
 member_count INTEGER NOT NULL CHECK(member_count>=0),
 PRIMARY KEY(set_id,position),UNIQUE(set_id,key)) WITHOUT ROWID;
CREATE TABLE dtc_membership_rows(
 set_id INTEGER NOT NULL,group_position INTEGER NOT NULL,position INTEGER NOT NULL CHECK(position>=0),
 entry_id INTEGER NOT NULL REFERENCES dtc_entries(id),
 PRIMARY KEY(set_id,group_position,position),
 FOREIGN KEY(set_id,group_position) REFERENCES dtc_membership_groups(set_id,position)) WITHOUT ROWID;
CREATE TABLE display_membership_sets(
 id INTEGER PRIMARY KEY,row_count INTEGER NOT NULL CHECK(row_count>=0));
CREATE TABLE display_membership_rows(
 set_id INTEGER NOT NULL REFERENCES display_membership_sets(id),
 position INTEGER NOT NULL CHECK(position>=0),
 row_id INTEGER NOT NULL REFERENCES data_list_rows(id),
 PRIMARY KEY(set_id,position)) WITHOUT ROWID;
CREATE TABLE customize_choice_definitions(id INTEGER PRIMARY KEY,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),name_string_index INTEGER NOT NULL,source_ordinal INTEGER NOT NULL,value INTEGER NOT NULL);
CREATE TABLE customize_choice_lists(id INTEGER PRIMARY KEY,entry_count INTEGER NOT NULL CHECK(entry_count>=0));
CREATE TABLE customize_choice_members(list_id INTEGER NOT NULL REFERENCES customize_choice_lists(id),position INTEGER NOT NULL,choice_id INTEGER NOT NULL REFERENCES customize_choice_definitions(id),PRIMARY KEY(list_id,position)) WITHOUT ROWID;
CREATE TABLE customize_executor_profiles(id INTEGER PRIMARY KEY,family_id INTEGER REFERENCES text_values(id),merge_id INTEGER REFERENCES text_values(id),read_current_id INTEGER REFERENCES text_values(id),session_requirement_id INTEGER REFERENCES text_values(id),support_gate_id INTEGER REFERENCES text_values(id),verify_id INTEGER REFERENCES text_values(id),write_id INTEGER REFERENCES text_values(id));
CREATE TABLE customize_group_definitions(id INTEGER PRIMARY KEY,body_type INTEGER NOT NULL,exception_flag INTEGER NOT NULL,exception_id INTEGER NOT NULL,group_id INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),name_string_index INTEGER NOT NULL,source_ordinal INTEGER NOT NULL);
CREATE TABLE customize_item_definitions(id INTEGER PRIMARY KEY,all_default_gate_u16_22 INTEGER NOT NULL,choice_list_key INTEGER NOT NULL,current_bit_end INTEGER NOT NULL,current_bit_start INTEGER NOT NULL,current_data_mode INTEGER NOT NULL,group_id INTEGER NOT NULL,item_aux_u16_0e INTEGER NOT NULL,item_id INTEGER NOT NULL,legacy_data_id INTEGER NOT NULL,merge_mode INTEGER NOT NULL,metadata_dword_04 INTEGER NOT NULL,name_id INTEGER REFERENCES text_values(id),name_string_index INTEGER NOT NULL,raw_u16_1e INTEGER NOT NULL,raw_u16_28 INTEGER NOT NULL,raw_u32_24 INTEGER NOT NULL,raw_u8_33 INTEGER NOT NULL,read_current INTEGER NOT NULL,source_ordinal INTEGER NOT NULL,support_bit_end INTEGER NOT NULL,support_bit_start INTEGER NOT NULL,support_mode INTEGER NOT NULL,support_selector INTEGER NOT NULL,target_category_id INTEGER NOT NULL,target_category_name_id INTEGER REFERENCES text_values(id),target_generation INTEGER NOT NULL,target_resolution_id INTEGER REFERENCES text_values(id),target_route_selector INTEGER NOT NULL,write_bit_end INTEGER NOT NULL,write_bit_start INTEGER NOT NULL,write_did INTEGER NOT NULL,write_variant INTEGER NOT NULL,choice_list_id INTEGER NOT NULL REFERENCES customize_choice_lists(id),executor_id INTEGER REFERENCES customize_executor_profiles(id));
CREATE TABLE vehicle_definitions(
 id INTEGER PRIMARY KEY,region_id INTEGER NOT NULL REFERENCES search_regions(id),
 vehicle_type INTEGER NOT NULL,name_id INTEGER NOT NULL REFERENCES text_values(id),
 install_count INTEGER NOT NULL,UNIQUE(region_id,vehicle_type));
CREATE TABLE vehicle_install_references(
 vehicle_id INTEGER NOT NULL REFERENCES vehicle_definitions(id),position INTEGER NOT NULL,
 install_set_id INTEGER NOT NULL,PRIMARY KEY(vehicle_id,position)) WITHOUT ROWID;
CREATE TABLE vehicle_connections(
 id INTEGER PRIMARY KEY,category_id INTEGER NOT NULL,comm_set_id INTEGER NOT NULL,
 frame_id INTEGER NOT NULL,route_selector INTEGER NOT NULL,has_route INTEGER NOT NULL);
CREATE TABLE install_set_definitions(id INTEGER PRIMARY KEY,member_count INTEGER NOT NULL);
CREATE TABLE install_set_members(
 definition_id INTEGER NOT NULL REFERENCES install_set_definitions(id),position INTEGER NOT NULL,
 connection_id INTEGER NOT NULL REFERENCES vehicle_connections(id),PRIMARY KEY(definition_id,position)) WITHOUT ROWID;
CREATE TABLE install_set_owners(
 region_id INTEGER NOT NULL REFERENCES search_regions(id),install_set_id INTEGER NOT NULL,
 definition_id INTEGER NOT NULL REFERENCES install_set_definitions(id),PRIMARY KEY(region_id,install_set_id)) WITHOUT ROWID;
CREATE TABLE vehicle_route_owners(
 region_id INTEGER NOT NULL REFERENCES search_regions(id),category_id INTEGER NOT NULL,route_selector INTEGER NOT NULL,
 definition_id INTEGER NOT NULL REFERENCES vehicle_route_definitions(id),
 PRIMARY KEY(region_id,category_id,route_selector)) WITHOUT ROWID;
CREATE TABLE vehicle_route_definitions(id INTEGER PRIMARY KEY,address_extension INTEGER NOT NULL,change_comm_if_case INTEGER NOT NULL,functional_address INTEGER NOT NULL,functional_address_field INTEGER NOT NULL,legislated_request_address INTEGER NOT NULL,protocol_info_id INTEGER NOT NULL,request_address INTEGER NOT NULL,request_address_field INTEGER NOT NULL,request_mask INTEGER NOT NULL,response_mask INTEGER NOT NULL,route_selector INTEGER NOT NULL,functional_request_address INTEGER,physical_request_address INTEGER,physical_response_address INTEGER,controller_id INTEGER REFERENCES text_values(id),request_address_field_semantics_id INTEGER REFERENCES text_values(id),transport_kind_id INTEGER REFERENCES text_values(id));
CREATE TABLE topology_buses(bus_index INTEGER PRIMARY KEY,name_id INTEGER NOT NULL REFERENCES text_values(id));
CREATE TABLE topology_placements(id INTEGER PRIMARY KEY,bus_index INTEGER NOT NULL REFERENCES topology_buses(bus_index),component_index INTEGER NOT NULL,domain_id INTEGER NOT NULL REFERENCES text_values(id),junction_id INTEGER NOT NULL REFERENCES text_values(id),gateway_count INTEGER NOT NULL);
CREATE TABLE topology_gateways(placement_id INTEGER NOT NULL REFERENCES topology_placements(id),position INTEGER NOT NULL,name_id INTEGER NOT NULL REFERENCES text_values(id),PRIMARY KEY(placement_id,position)) WITHOUT ROWID;
CREATE TABLE topology_variants(id INTEGER PRIMARY KEY,group_count INTEGER NOT NULL,placement_count INTEGER NOT NULL);
CREATE TABLE topology_variant_groups(variant_id INTEGER NOT NULL REFERENCES topology_variants(id),position INTEGER NOT NULL,group_id INTEGER NOT NULL,PRIMARY KEY(variant_id,position)) WITHOUT ROWID;
CREATE TABLE topology_variant_placements(variant_id INTEGER NOT NULL REFERENCES topology_variants(id),position INTEGER NOT NULL,placement_id INTEGER NOT NULL REFERENCES topology_placements(id),PRIMARY KEY(variant_id,position)) WITHOUT ROWID;
CREATE TABLE topology_networks(id INTEGER PRIMARY KEY,can_car_id INTEGER NOT NULL UNIQUE,option_count INTEGER NOT NULL,variant_count INTEGER NOT NULL);
CREATE TABLE topology_network_variants(network_id INTEGER NOT NULL REFERENCES topology_networks(id),position INTEGER NOT NULL,variant_id INTEGER NOT NULL REFERENCES topology_variants(id),PRIMARY KEY(network_id,position)) WITHOUT ROWID;
CREATE TABLE topology_vehicle_views(id INTEGER PRIMARY KEY,vehicle_id INTEGER NOT NULL REFERENCES vehicle_definitions(id),network_id INTEGER NOT NULL REFERENCES topology_networks(id),UNIQUE(vehicle_id,network_id));
CREATE TABLE ecu_catalog_definitions(id INTEGER PRIMARY KEY,data BLOB NOT NULL);
CREATE TABLE ecu_catalog_bindings(
 document_id INTEGER PRIMARY KEY REFERENCES source_documents(id),
 definition_id INTEGER NOT NULL REFERENCES ecu_catalog_definitions(id),
 category BLOB NOT NULL);
CREATE VIRTUAL TABLE items_fts USING fts5(
  name, detail, identifier, content='items', content_rowid='id',
  tokenize='unicode61'
);
CREATE INDEX ecus_key ON ecus(region, key COLLATE NOCASE);
CREATE UNIQUE INDEX search_records_value ON search_records(kind_id,identifier_id,name_id,detail_id);
CREATE VIEW search_text AS SELECT id,value FROM text_values;
CREATE VIEW items AS SELECT o.id,g.name AS region,o.category_id,k.name AS kind,
 x.value AS identifier,n.value AS name,t.value AS detail
 FROM search_regions g CROSS JOIN item_occurrences o ON g.id=o.region_id
 JOIN search_records r ON r.id=o.record_id JOIN search_kinds k ON k.id=r.kind_id
 JOIN search_text x ON x.id=r.identifier_id JOIN search_text n ON n.id=r.name_id
 JOIN search_text t ON t.id=r.detail_id;
CREATE VIEW owner_items AS SELECT o.id,g.name AS region,r.category_id,k.name AS kind,
 x.value AS identifier,n.value AS name,t.value AS detail
 FROM search_regions g CROSS JOIN owner_ranges r ON r.region_id=g.id
 CROSS JOIN item_occurrences o NOT INDEXED ON o.id BETWEEN r.first_id AND r.last_id
 AND o.region_id=r.region_id AND o.category_id=r.category_id
 JOIN search_records s ON s.id=o.record_id JOIN search_kinds k ON k.id=s.kind_id
 JOIN search_text x ON x.id=s.identifier_id JOIN search_text n ON n.id=s.name_id
 JOIN search_text t ON t.id=s.detail_id;
CREATE VIEW runtime_documents AS SELECT member,data FROM source_documents;
CREATE VIEW source_members AS SELECT id,member FROM source_documents;
CREATE VIEW item_source_refs AS SELECT id AS item_id,document_id AS member_id,pointer_id FROM item_occurrences;
CREATE VIEW item_sources AS SELECT o.id AS item_id,d.member,p.pointer
 FROM item_occurrences o JOIN source_documents d ON d.id=o.document_id
 JOIN source_pointers p ON p.id=o.pointer_id;
CREATE VIEW item_counts AS SELECT g.name AS region,c.category_id,k.name AS kind,c.count
 FROM item_count_rows c JOIN search_regions g ON g.id=c.region_id
 JOIN search_kinds k ON k.id=c.kind_id;
CREATE VIEW vehicles AS 
SELECT g.name AS region,v.vehicle_type,t.value AS name,
 (SELECT json_group_array(install_set_id)
  FROM (SELECT install_set_id FROM vehicle_install_references r
        WHERE r.vehicle_id=v.id ORDER BY position)) AS install_set_ids
FROM search_regions g JOIN vehicle_definitions v ON v.region_id=g.id
JOIN text_values t ON t.id=v.name_id;
CREATE VIEW vehicle_ecus AS 
SELECT DISTINCT g.name AS region,v.vehicle_type,s.install_set_id,
 c.category_id,c.route_selector,c.frame_id AS connection_frame_id,
 CASE WHEN c.has_route=1 THEN CAST(c.category_id AS TEXT)||':'||CAST(c.route_selector AS TEXT) ELSE NULL END AS route_key,
 d.physical_request_address AS request_address,d.address_extension,k.value AS transport_kind
FROM search_regions g
JOIN vehicle_definitions v ON v.region_id=g.id
JOIN vehicle_install_references r ON r.vehicle_id=v.id
JOIN install_set_owners s ON s.region_id=v.region_id AND s.install_set_id=r.install_set_id
JOIN install_set_members m ON m.definition_id=s.definition_id
JOIN vehicle_connections c ON c.id=m.connection_id
LEFT JOIN vehicle_route_owners o ON c.has_route=1 AND o.region_id=v.region_id
 AND o.category_id=c.category_id AND o.route_selector=c.route_selector
LEFT JOIN vehicle_route_definitions d ON d.id=o.definition_id
LEFT JOIN text_values k ON k.id=d.transport_kind_id;
CREATE VIEW vehicle_ecu_counts AS 
SELECT g.name AS region,v.vehicle_type,count(DISTINCT c.category_id) AS ecu_count
FROM search_regions g JOIN vehicle_definitions v ON v.region_id=g.id
JOIN vehicle_install_references r ON r.vehicle_id=v.id
JOIN install_set_owners s ON s.region_id=v.region_id AND s.install_set_id=r.install_set_id
JOIN install_set_members m ON m.definition_id=s.definition_id
JOIN vehicle_connections c ON c.id=m.connection_id
GROUP BY g.name,v.vehicle_type;
