package catalog

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"
)

// These expectations come from decoded OEM rows, not project_document's output.
// They include facts a lossy product projection would discard.
// Runs against the shipped catalog by default; the environment variable can
// select a newly generated artifact before it replaces the packaged database.
func TestDecodedSourceCoverage(t *testing.T) {
	path := os.Getenv("TOYO_TEST_SOURCE_COVERAGE_CATALOG")
	shipped := path == ""
	if shipped {
		path = "../../catalog/catalog.sqlite"
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Skip("shipped catalog is missing; restore catalog/catalog.sqlite and run git lfs pull to exercise decoded-source coverage")
		}
	}
	d, err := Open(path)
	if err != nil {
		if shipped && errors.Is(err, ErrLFSPointer) {
			t.Skip(err.Error())
		}
		t.Fatal(err)
	}
	defer d.Close()
	ctx := context.Background()
	cases := []struct {
		member, pointer string
		want            map[string]any
	}{
		// CVT_P4DK2 table41 record0 has nonzero bytes 70:72 outside decoded fields.
		{"catalogs/NA/607.json", "/variable_drive/active_tests/0/uninterpreted_bytes", map[string]any{"0": "0000000000000000000000000000000000000000", "70": "0100", "84": "00", "89": "000000"}},
		// Literal nonzero OEM bytes outside the decoded legacy-generation fields.
		{"catalogs/EU/118.json", "/legacy_records/79/uninterpreted_bytes", map[string]any{"70_73": "07000000"}},
		{"catalogs/EU/234.json", "/dynamic_lsb/17/uninterpreted_bytes", map[string]any{"14_16": "360302"}},
		{"catalogs/EU/234.json", "/legacy_records/1487/dynamic_lsb_records/0/uninterpreted_bytes", map[string]any{"14_16": "360302"}},
		{"catalogs/EU/6026.json", "/p6_rob_source/behavior_codes/0/uninterpreted_bytes", map[string]any{"65_67": "000000"}},
		{"catalogs/EU/6026.json", "/p6_rob_source/signals/0/uninterpreted_bytes", map[string]any{"38_39": "0000", "43_47": "0000000000"}},
		{"catalogs/EU/118.json", "/dtc_source/5", map[string]any{"code": "C1260", "enabled": false, "enabled_value": json.Number("0"), "detail_table": json.Number("47"), "detail_lookup_key": json.Number("21088")}},
		{"catalogs/EU/372.json", "/monitor_source/10289", map[string]any{"did": json.Number("4126"), "fixed_length": json.Number("6"), "support_mode": json.Number("0"), "table": json.Number("90")}},
		{"catalogs/EU/372.json", "/monitor_source/8823", map[string]any{"did": json.Number("62209"), "maximum_payload_length": json.Number("40"), "kind": "dynamic_did"}},
		{"catalogs/EU/188.json", "/native_ddr/monitors/0", map[string]any{"name": "Trigger ID", "bit_start": json.Number("0"), "bit_end": json.Number("7"), "data_max": json.Number("255"), "uninterpreted_bytes": map[string]any{"42_43": "0001", "46_47": "0000", "8_11": "00000000"}}},
		{"catalogs/EU/6026.json", "/monitor_source/428", map[string]any{"did": json.Number("4164"), "exception_handler_flag": json.Number("2"), "exception_handler_id": json.Number("1089"), "fixed_length": json.Number("2")}},
		{"catalogs/NA/1.json", "/physical_catalog/patterns/0", map[string]any{"name": "Dummy", "unassigned_fields": map[string]any{"14_15": "0100", "18_22": "0000000001", "8_11": "00000000"}}},
		{"catalogs/EU/444.json", "/native_ddr/case_data_ids/0", map[string]any{"case_did": json.Number("5377"), "uninterpreted_bytes": map[string]any{"9": "01", "11": "00"}}},
		{"catalogs/EU/444.json", "/native_ddr/header_data_ids/0", map[string]any{"header_did": json.Number("5136"), "header_parameter": json.Number("62")}},
	}
	for _, tc := range cases {
		t.Run(tc.member+tc.pointer, func(t *testing.T) {
			v, e := d.sourceValue(ctx, tc.member, tc.pointer)
			if e != nil {
				t.Fatal(e)
			}
			v, e = d.expandMetadata(ctx, v, make(referenceStack))
			if e != nil {
				t.Fatal(e)
			}
			row, ok := v.(map[string]any)
			if !ok {
				t.Fatal("source row missing")
			}
			for k, want := range tc.want {
				if !reflect.DeepEqual(row[k], want) {
					t.Errorf("%s: got %#v want %#v", k, row[k], want)
				}
			}
		})
	}
	// Spe files are independent sources, not admitted Gen overrides. The JP
	// HEV Brake source exists despite having no same-name Gen file.
	for region, count := range map[string]int{"EU": 5, "JP": 4, "NA": 5} {
		value, err := d.sourceValue(ctx, "index.json", "/regions/"+region+"/ecu_overlays")
		if err != nil {
			t.Fatal(err)
		}
		value, err = d.expandMetadata(ctx, value, make(referenceStack))
		if err != nil {
			t.Fatal(err)
		}
		overlays, ok := value.([]any)
		if !ok || len(overlays) != count {
			t.Fatalf("%s overlays: got %T/%v", region, value, value)
		}
		foundJPBrake := false
		for _, value := range overlays {
			entry := value.(map[string]any)
			scope := entry["source_scope"].(map[string]any)
			if scope["region"] != region || scope["family"] != "Spe" {
				t.Fatal("incorrect overlay scope", scope)
			}
			if entry["activation"].(map[string]any)["status"] != "unresolved" {
				t.Fatal("unproved overlay activation claimed", entry)
			}
			rows := entry["operation_freeze"].([]any)
			if len(rows) != 4 {
				t.Fatal("missing overlay rows", scope)
			}
			for i, value := range rows {
				row := value.(map[string]any)
				if row["unassigned_bytes_2_4"] != []string{"010000", "020000", "030000", "040000"}[i] || row["group_selector"] != catalogNumber(1) {
					t.Fatal("overlay undecoded fields changed", row)
				}
				if row["table"] != catalogNumber(54) || row["record"] != catalogNumber(int64(i)) || row["freeze_frame_number"] != catalogNumber(int64(15-i)) {
					t.Fatal("overlay order/facts changed", row)
				}
			}
			if region == "JP" && scope["database"] == "HEV Brake_P4DK.ddb" {
				foundJPBrake = true
			}
		}
		if region == "JP" && !foundJPBrake {
			t.Fatal("Spe-only JP HEV Brake source omitted")
		}
	}
	rows, err := d.SearchKind(ctx, "EU", 188, "native-ddr-monitors", "Trigger ID", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) == 0 {
		t.Error("decoded airbag recorder signals no longer searchable")
	}
	overlays, err := d.SearchKind(ctx, "JP", 0, "ecu-overlay", "HEV", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(overlays) == 0 {
		t.Error("Spe-only JP HEV Brake source is not searchable")
	}

}
