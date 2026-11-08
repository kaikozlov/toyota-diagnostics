package diagnostic

import (
	"context"
	"strings"
	"testing"
)

func profileFixture() map[string]any {
	cat := map[string]any{"category_id": 405, "name": "EMPS", "database": "EMPS_P5.ddb", "generation": 20, "catalog_member": "catalogs/NA/405.json"}
	row := func(frame int) any {
		return map[string]any{"category_id": 405, "connection_route_selector": 18, "connection_frame_id": frame, "route_key": "405:18"}
	}
	return map[string]any{"default_region": "NA", "regions": map[string]any{"NA": map[string]any{
		"selected_categories": map[string]any{"405": cat, "999": map[string]any{"name": "Unresolved", "catalog_member": "catalogs/NA/999.json"}},
		"vehicles":            map[string]any{"1": map[string]any{"vehicle_type": 1, "name": "Example", "install_set_ids": []any{10, 11}}, "2": map[string]any{"vehicle_type": 2, "name": "Example Hybrid", "install_set_ids": []any{10}}},
		"install_sets":        map[string]any{"10": []any{row(1), row(2), map[string]any{"category_id": 999, "connection_route_selector": 22, "connection_frame_id": 0}}, "11": []any{row(1)}},
		"routes":              map[string]any{"405:18": map[string]any{"request_address": 0x123456, "physical_request_address": 0x750, "address_extension": 0x0a, "transport_kind": "iso15765-phase-family"}},
		"commsets":            map[string]any{"1": map[string]any{"receive_timeout": 1020}},
		"session_control":     map[string]any{"per_category": map[string]any{"405": map[string]any{"default_session": map[string]any{"comm_set": 1}, "extended_session": map[string]any{"comm_set": 1}}}},
	}}}
}
func TestProfileCatalogAndVehicle(t *testing.T) {
	p, e := profileFromIndex(profileFixture(), "", "", 0)
	if e != nil || len(p.ECUs) != 2 || p.ECUs[0].RouteResolved {
		t.Fatal(p, e)
	}
	p, e = profileFromIndex(profileFixture(), "na", "Example", 2)
	if e != nil {
		t.Fatal(e)
	}
	if p.Region != "NA" || p.Bus != 2 || len(p.Candidates) != 3 || len(p.ECUs) != 2 {
		t.Fatal(p)
	}
	ecu := p.ECUs[0]
	if ecu.Address != 0x750 || ecu.SubAddr == nil || *ecu.SubAddr != 0xa || !ecu.RouteResolved || ecu.Generation != 20 {
		t.Fatal(ecu)
	}
	if p.ECUs[1].RouteResolved {
		t.Fatal("unresolved route became executable")
	}
	for _, q := range []string{"405", "000405", "emps", "0x750"} {
		got, e := p.LookupECU(q)
		if e != nil || got.CategoryID != 405 {
			t.Fatal(q, got, e)
		}
	}
	id, row, e := p.SessionCommset(ecu)
	if e != nil || id != 1 || profileInt(row["receive_timeout"]) != 1020 {
		t.Fatal(id, row, e)
	}
	if _, e := profileFromIndex(profileFixture(), "XX", "", 0); e == nil {
		t.Fatal("unknown region")
	}
	if _, e := profileFromIndex(profileFixture(), "NA", "Example Hy", 0); e != nil {
		t.Fatal(e)
	}
	if _, e := profileFromIndex(profileFixture(), "NA", "Ex", 0); e == nil || !strings.Contains(e.Error(), "ambiguous") {
		t.Fatal(e)
	}
	if _, e := profileFromIndex(profileFixture(), "NA", "missing", 0); e == nil {
		t.Fatal("unknown vehicle")
	}
}
func TestProfileDIDResolution(t *testing.T) {
	p, e := profileFromIndex(profileFixture(), "NA", "", 0)
	if e != nil {
		t.Fatal(e)
	}
	ecu := p.ECUs[0]
	p.categories[405] = map[string]any{"dids": map[string]any{
		"0x1037": []any{map[string]any{"name": "Steering Angle"}, map[string]any{"name": "Other"}},
		"0x1038": []any{map[string]any{"name": "Steering Angle Speed"}},
	}}
	for _, q := range []string{"0x1037", "1037", "4151", "Steering Angle", "Other"} {
		n, rows, e := p.ResolveDID(context.Background(), ecu, q)
		want := 0x1037
		if q == "4151" {
			want = 0x4151
		}
		if e != nil || n != want {
			t.Fatal(q, n, e)
		}
		if want == 0x1037 && len(rows) != 2 {
			t.Fatal(rows)
		}
	}
	if _, _, e := p.ResolveDID(context.Background(), ecu, "Steering"); e == nil {
		t.Fatal("ambiguous DID")
	}
	if _, _, e := p.ResolveDID(context.Background(), ecu, "unknown"); e == nil {
		t.Fatal("unknown DID")
	}
	if _, _, e := p.ResolveDID(context.Background(), ecu, "0x10000"); e == nil {
		t.Fatal("oversize DID")
	}
	if _, e := p.LookupECU("missing"); e == nil {
		t.Fatal("unknown ECU")
	}
	p.ECUs = append(p.ECUs, p.ECUs[0])
	if _, e := p.LookupECU("405"); e == nil {
		t.Fatal("ambiguous ECU")
	}
}
func TestProfileExactECUKeyWinsOverDisplayNames(t *testing.T) {
	p := &Profile{ECUs: []EcuSpec{{Key: "eps", Name: "EMPS", CategoryID: 405}, {Key: "eps_can", Name: "EPS", CategoryID: 55}, {Key: "emps_13", Name: "EPS", CategoryID: 13}}}
	got, err := p.LookupECU("eps")
	if err != nil || got.CategoryID != 405 {
		t.Fatal(got, err)
	}
	got, err = p.LookupECU("EPS")
	if err != nil || got.CategoryID != 405 {
		t.Fatal(got, err)
	}
}
func TestProfileSessionCommsetConsistency(t *testing.T) {
	p, e := profileFromIndex(profileFixture(), "NA", "", 0)
	if e != nil {
		t.Fatal(e)
	}
	ecu := p.ECUs[0]
	category := profileMap(profileMap(p.SessionControl()["per_category"])["405"])
	category["keepalive"] = map[string]any{"comm_set": 2}
	if _, _, e := p.SessionCommset(ecu); e == nil {
		t.Fatal("mixed commsets")
	}
	delete(category, "keepalive")
	delete(profileMap(p.Regional["commsets"]), "1")
	if _, _, e := p.SessionCommset(ecu); e == nil {
		t.Fatal("missing commset")
	}
}
