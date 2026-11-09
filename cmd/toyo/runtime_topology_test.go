package main

import (
	"context"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
)

func TestCANTopologyMaterializesSelectedVehicleAfterNewProfile(t *testing.T) {
	// An unrelated invalid marker proves the command does not decode every
	// vehicle's topology as a side effect of opening a profile.
	index := map[string]any{"default_region": "NA", "regions": map[string]any{"NA": map[string]any{
		"selected_categories": map[string]any{},
		"vehicles":            map[string]any{"7": map[string]any{"vehicle_type": 7, "name": "Toyota", "install_set_ids": []any{}}},
		"install_sets":        map[string]any{}, "routes": map[string]any{},
		"can_topology": map[string]any{
			"7": []any{map[string]any{"vehicle_type": 7, "vehicle_name": "Toyota", "variants": []any{map[string]any{"components": []any{map[string]any{"domain_name": "Radar"}}}}}},
			"8": []any{map[string]any{"$can_topology": 999999}},
		}}}}
	c := openDocument(t, "index.json", index)
	p, err := diagnostic.NewProfile(context.Background(), c, "NA", "7", 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.Regional["can_topology"] != nil {
		t.Fatal("profile index must not carry CAN topology")
	}
	r := &runtimeApp{ctx: context.Background(), db: c, profile: p, opt: runtimeOptions{json: true}}
	out, err := capture(func() error { return r.canCommand([]string{"can", "topology"}) })
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "Radar") || !strings.Contains(out, "Toyota") || strings.Contains(out, "$can_topology") {
		t.Fatal(out)
	}
	if r.transport != nil {
		t.Fatal("offline topology opened hardware")
	}
}
