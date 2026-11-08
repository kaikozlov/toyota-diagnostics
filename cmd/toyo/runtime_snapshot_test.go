package main

import (
	"context"
	"encoding/hex"
	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"testing"
)

func TestVINResolutionCanonicalFields(t *testing.T) {
	vin := "AAAAAAAKASA123456"
	prefix := hex.EncodeToString([]byte(vin[:11]))
	regional := map[string]any{"vin_decision": map[string]any{"rows": []map[string]any{{"category_id": 372, "route_selector": 18, "vehicle_type": 12704, "flags": 0, "vin_prefix_hex": prefix}}}, "selected_categories": map[string]any{"372": map[string]any{"generation": 20}}, "vehicle_resolver_dispatch": map[string]any{"vin10_generation_low5": map[string]any{"20": "phase5"}}, "vehicles": map[string]any{"12704": map[string]any{"vehicle_type": 12704, "name": "Camry HV"}}}
	rows, e := resolveVINRows(regional, vin, 0)
	if e != nil || len(rows) != 1 || rows[0]["resolution_complete"] != true {
		t.Fatal(rows, e)
	}
	regional["vehicle_resolver_dispatch"] = map[string]any{"vin10_generation_low5": map[string]any{"20": "phase4"}}
	rows, e = resolveVINRows(regional, vin, 0)
	if e != nil || rows[0]["resolution_complete"] != false {
		t.Fatal(rows, e)
	}
}
func TestEnsureSelectedVehicleDoesNotTouchHardware(t *testing.T) {
	r := &runtimeApp{ctx: context.Background(), profile: &diagnostic.Profile{Vehicle: map[string]any{"vehicle_type": 1}}}
	if e := r.ensureVehicle(); e != nil || r.transport != nil {
		t.Fatal(e)
	}
}
func TestHealthCompareBadFileBeforeHardware(t *testing.T) {
	r := &runtimeApp{ctx: context.Background(), profile: &diagnostic.Profile{}, opt: runtimeOptions{values: map[string][]string{"compare": {"/no/such/snapshot.json"}}}}
	if e := r.healthCheck(); e == nil || r.transport != nil {
		t.Fatal("must reject missing snapshot before connecting", e)
	}
}
