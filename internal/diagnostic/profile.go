package diagnostic

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

type EcuSpec struct {
	Key           string         `json:"key"`
	Name          string         `json:"name"`
	CategoryID    int            `json:"category_id"`
	Address       uint32         `json:"address"`
	SubAddr       *byte          `json:"sub_addr,omitempty"`
	Generation    int            `json:"generation"`
	RouteResolved bool           `json:"route_resolved"`
	TransportKind string         `json:"transport_kind"`
	Route         map[string]any `json:"route,omitempty"`
}
type Profile struct {
	DB             *catalog.DB
	Region         string
	Index, Vehicle map[string]any
	// Regional holds the regional index sections read so far. Section reads
	// the others on first use.
	Regional   map[string]any
	Bus        int
	ECUs       []EcuSpec
	Candidates []map[string]any
	source     regionalIndex
	mu         sync.Mutex
	sectionMu  sync.Mutex
	categories map[int]map[string]any
}

func profileMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func profileText(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func profileRows(v any) []map[string]any {
	out := []map[string]any{}
	switch a := v.(type) {
	case []any:
		for _, x := range a {
			if m := profileMap(x); m != nil {
				out = append(out, m)
			}
		}
	case []map[string]any:
		return a
	}
	return out
}
func profileInt(v any) int { n, _ := decodeInt(map[string]any{"n": v}, "n"); return int(n) }
func profileClone(m map[string]any) map[string]any {
	n := make(map[string]any, len(m))
	for k, v := range m {
		n[k] = v
	}
	return n
}

func NewProfile(ctx context.Context, db *catalog.DB, region, vehicle string, bus int) (*Profile, error) {
	if bus < 0 || bus > 3 {
		return nil, fmt.Errorf("Panda diagnostic bus must be 0..3, got %d", bus)
	}
	index, e := db.Index(ctx)
	if e != nil {
		return nil, e
	}
	if region == "" {
		region = profileText(index["default_region"])
	}
	p, e := buildProfile(ctx, index, region, vehicle, bus, catalogRegion{db})
	if e != nil {
		return nil, e
	}
	p.DB = db
	// Reuse the public catalog's canonical ECU keys; do not maintain a second alias table.
	ids := make([]int, len(p.ECUs))
	for i, ecu := range p.ECUs {
		ids[i] = ecu.CategoryID
	}
	keys, e := db.ECUKeys(ctx, p.Region, ids)
	if e != nil {
		return nil, e
	}
	used := map[string]bool{}
	for i := range p.ECUs {
		base := keys[p.ECUs[i].CategoryID]
		if base == "" {
			base = p.ECUs[i].Key
		}
		key := base
		for suffix := 2; used[key]; suffix++ {
			key = fmt.Sprintf("%s_%d", base, suffix)
		}
		used[key] = true
		p.ECUs[i].Key = key
	}
	return p, nil
}

// regionalIndex reads one region's index a section or a few entries at a
// time, so a profile decodes only what it uses.
type regionalIndex interface {
	// section is nil when the region has no such section.
	section(ctx context.Context, region, name string) (any, error)
	// entries is nil when the region has no such section.
	entries(ctx context.Context, region, section string, keys []string) (map[string]any, error)
	// vehicles holds each vehicle's vehicle_type and name. Given types, it
	// may leave out vehicles of other types.
	vehicles(ctx context.Context, region string, types ...int64) (map[string]any, error)
}

type catalogRegion struct{ db *catalog.DB }

func (c catalogRegion) section(ctx context.Context, region, name string) (any, error) {
	return c.db.RegionalSection(ctx, region, name)
}
func (c catalogRegion) entries(ctx context.Context, region, section string, keys []string) (map[string]any, error) {
	return c.db.RegionalEntries(ctx, region, section, keys)
}
func (c catalogRegion) vehicles(ctx context.Context, region string, types ...int64) (map[string]any, error) {
	return c.db.RegionalVehicles(ctx, region, types...)
}

// Section returns one regional index section, read on first use.
func (p *Profile) Section(ctx context.Context, name string) (any, error) {
	p.sectionMu.Lock()
	defer p.sectionMu.Unlock()
	if value, ok := p.Regional[name]; ok || p.source == nil {
		return value, nil
	}
	value, e := p.source.section(ctx, p.Region, name)
	if e != nil {
		return nil, e
	}
	if p.Regional == nil {
		p.Regional = map[string]any{}
	}
	p.Regional[name] = value
	return value, nil
}

// SelectedCategory returns one category's entry in the regional index
// without reading the others.
func (p *Profile) SelectedCategory(ctx context.Context, id int) (map[string]any, error) {
	key := strconv.Itoa(id)
	p.sectionMu.Lock()
	all, ok := p.Regional["selected_categories"]
	p.sectionMu.Unlock()
	if ok || p.source == nil {
		return profileMap(profileMap(all)[key]), nil
	}
	rows, e := p.source.entries(ctx, p.Region, "selected_categories", []string{key})
	if e != nil {
		return nil, e
	}
	return profileMap(rows[key]), nil
}

func buildProfile(ctx context.Context, index map[string]any, region, vehicle string, bus int, source regionalIndex) (*Profile, error) {
	if region == "" {
		region = "NA"
	}
	region = strings.ToUpper(region)
	present, e := source.entries(ctx, region, "selected_categories", nil)
	if e != nil {
		return nil, e
	}
	if present == nil {
		return nil, fmt.Errorf("Toyota %s index has no selected category catalog index", region)
	}
	p := &Profile{Region: region, Index: index, Regional: map[string]any{}, Bus: bus, source: source, categories: map[int]map[string]any{}}
	if vehicle != "" {
		n, nerr := strconv.ParseInt(vehicle, 0, 64)
		types := []int64{}
		if nerr == nil {
			types = append(types, n)
		}
		vehicles, e := source.vehicles(ctx, region, types...)
		if e != nil {
			return nil, e
		}
		exact, fuzzy := []string{}, []string{}
		for key, v := range vehicles {
			row := profileMap(v)
			if row == nil {
				continue
			}
			name := profileText(row["name"])
			if nerr == nil {
				if profileInt(row["vehicle_type"]) == int(n) {
					exact = append(exact, key)
				}
			} else if strings.EqualFold(name, vehicle) {
				exact = append(exact, key)
			} else if strings.Contains(strings.ToLower(name), strings.ToLower(vehicle)) {
				fuzzy = append(fuzzy, key)
			}
		}
		matches := exact
		if len(matches) == 0 {
			matches = fuzzy
		}
		if len(matches) != 1 {
			if len(matches) == 0 {
				return nil, fmt.Errorf("no Toyota %s vehicle matches %q", region, vehicle)
			}
			return nil, fmt.Errorf("ambiguous Toyota vehicle %q: %d matches; use vehicle type", vehicle, len(matches))
		}
		chosen, e := source.entries(ctx, region, "vehicles", matches)
		if e != nil {
			return nil, e
		}
		p.Vehicle = profileMap(chosen[matches[0]])
		ids, _ := p.Vehicle["install_set_ids"].([]any)
		setKeys := make([]string, len(ids))
		for i, id := range ids {
			setKeys[i] = strconv.Itoa(profileInt(id))
		}
		sets, e := source.entries(ctx, region, "install_sets", setKeys)
		if e != nil {
			return nil, e
		}
		seen := map[string]bool{}
		categoryKeys, routeKeys := []string{}, []string{}
		for _, id := range ids {
			for _, raw := range profileRows(sets[strconv.Itoa(profileInt(id))]) {
				cid := profileInt(raw["category_id"])
				selector := profileInt(raw["connection_route_selector"])
				frame := profileInt(raw["connection_frame_id"])
				routeKey := profileText(raw["route_key"])
				identity := fmt.Sprintf("%d:%d:%d:%s", cid, selector, frame, routeKey)
				if seen[identity] {
					continue
				}
				seen[identity] = true
				row := profileClone(raw)
				row["install_set_id"] = profileInt(id)
				p.Candidates = append(p.Candidates, row)
				categoryKeys = append(categoryKeys, strconv.Itoa(cid))
				routeKeys = append(routeKeys, routeKey)
			}
		}
		categories, e := source.entries(ctx, region, "selected_categories", categoryKeys)
		if e != nil {
			return nil, e
		}
		routes, e := source.entries(ctx, region, "routes", routeKeys)
		if e != nil {
			return nil, e
		}
		for i, row := range p.Candidates {
			cat := profileMap(categories[categoryKeys[i]])
			for _, k := range []string{"generation", "database", "short_name", "name", "support_family", "support_mode", "support_plugin_single", "support_plugin_multi", "catalog_available"} {
				if v, ok := cat[k]; ok {
					row[k] = v
				}
			}
			if route := profileMap(routes[routeKeys[i]]); route != nil {
				row["transport_route"] = route
			}
		}
		sort.SliceStable(p.Candidates, func(i, j int) bool {
			a, b := p.Candidates[i], p.Candidates[j]
			for _, k := range []string{"category_id", "connection_route_selector", "connection_frame_id"} {
				if profileInt(a[k]) != profileInt(b[k]) {
					return profileInt(a[k]) < profileInt(b[k])
				}
			}
			return false
		})
		endpoints := map[string]bool{}
		used := map[string]bool{}
		for _, row := range p.Candidates {
			route := profileMap(row["transport_route"])
			cid := profileInt(row["category_id"])
			// Preserve unresolved candidates for inspection, never synthesize a request ID.
			address := profileInt(route["physical_request_address"])
			if route["physical_request_address"] == nil {
				address = profileInt(route["request_address"])
			}
			extension := profileInt(route["address_extension"])
			identity := fmt.Sprintf("%d:%d:%d:%s", cid, address, extension, profileText(route["transport_kind"]))
			if endpoints[identity] {
				continue
			}
			endpoints[identity] = true
			key := profileCategoryKey(cid, row)
			base := key
			for suffix := 2; used[key]; suffix++ {
				key = fmt.Sprintf("%s_%d", base, suffix)
			}
			used[key] = true
			spec := EcuSpec{Key: key, Name: profileCategoryName(cid, row), CategoryID: cid, Address: uint32(address), Generation: profileInt(row["generation"]), RouteResolved: route != nil && route["request_address"] != nil, TransportKind: profileText(route["transport_kind"]), Route: route}
			if extension != 0 {
				if extension < 0 || extension > 255 {
					return nil, fmt.Errorf("category %d has invalid address extension %d", cid, extension)
				}
				b := byte(extension)
				spec.SubAddr = &b
			}
			p.ECUs = append(p.ECUs, spec)
		}
	} else {
		section, e := source.section(ctx, region, "selected_categories")
		if e != nil {
			return nil, e
		}
		p.Regional["selected_categories"] = section
		categories := profileMap(section)
		ids := []int{}
		for key, value := range categories {
			row := profileMap(value)
			if row != nil && profileText(row["catalog_member"]) != "" {
				id, e := strconv.Atoi(key)
				if e != nil {
					return nil, e
				}
				ids = append(ids, id)
			}
		}
		sort.Ints(ids)
		for _, id := range ids {
			row := profileMap(categories[strconv.Itoa(id)])
			p.ECUs = append(p.ECUs, EcuSpec{Key: profileCategoryKey(id, row), Name: profileCategoryName(id, row), CategoryID: id, Generation: profileInt(row["generation"])})
		}
	}
	return p, nil
}

var profilePhase = regexp.MustCompile("(?i)_P[456].*$")
var profileNonWord = regexp.MustCompile("[^0-9A-Za-z]+")

func profileCategoryKey(id int, row map[string]any) string {
	s := profileText(row["short_name"])
	if s == "" {
		s = strings.TrimSuffix(filepath.Base(profileText(row["database"])), filepath.Ext(profileText(row["database"])))
	}
	s = strings.ToLower(strings.Trim(profileNonWord.ReplaceAllString(profilePhase.ReplaceAllString(s, ""), "_"), "_"))
	if s == "" {
		s = fmt.Sprintf("category_%d", id)
	}
	return s
}
func profileCategoryName(id int, row map[string]any) string {
	for _, key := range []string{"name", "short_name"} {
		if s := profileText(row[key]); s != "" {
			return s
		}
	}
	return fmt.Sprintf("Category %d", id)
}

func (p *Profile) Category(ctx context.Context, ecu EcuSpec) (map[string]any, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if row, ok := p.categories[ecu.CategoryID]; ok {
		return row, nil
	}
	meta, e := p.SelectedCategory(ctx, ecu.CategoryID)
	if e != nil {
		return nil, e
	}
	member := profileText(meta["catalog_member"])
	if member == "" {
		return nil, fmt.Errorf("category %d has no catalog document", ecu.CategoryID)
	}
	row, e := p.DB.Document(ctx, member)
	if e != nil {
		return nil, e
	}
	if p.categories == nil {
		p.categories = map[int]map[string]any{}
	}
	p.categories[ecu.CategoryID] = row
	return row, nil
}
func (p *Profile) LookupECU(query string) (EcuSpec, error) {
	text := strings.TrimSpace(query)
	matches := []EcuSpec{}
	isAddress := strings.HasPrefix(strings.ToLower(text), "0x")
	base := 10
	if isAddress {
		base = 0
	}
	number, err := strconv.ParseInt(text, base, 64)
	if err != nil {
		for _, ecu := range p.ECUs {
			if strings.EqualFold(text, ecu.Key) {
				matches = append(matches, ecu)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		if len(matches) > 1 {
			return EcuSpec{}, fmt.Errorf("ambiguous ECU key %q", query)
		}
	}
	for _, e := range p.ECUs {
		match := strings.EqualFold(text, e.Key) || strings.EqualFold(text, e.Name)
		if err == nil {
			if isAddress {
				match = e.RouteResolved && uint32(number) == e.Address
			} else {
				match = int(number) == e.CategoryID
			}
		}
		if match {
			matches = append(matches, e)
		}
	}
	if len(matches) == 0 && p.DB != nil && !isAddress {
		row, e := p.DB.ECU(context.Background(), p.Region, text)
		if e == nil {
			for _, spec := range p.ECUs {
				if spec.CategoryID == row.CategoryID {
					matches = append(matches, spec)
				}
			}
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	if len(matches) == 0 {
		return EcuSpec{}, fmt.Errorf("no ECU matches %q", query)
	}
	keys := []string{}
	for _, e := range matches {
		keys = append(keys, e.Key)
	}
	return EcuSpec{}, fmt.Errorf("ambiguous ECU %q: %s", query, strings.Join(keys, ", "))
}
func profileHexKey(text string) (int, error) {
	text = strings.TrimSpace(text)
	base := 10
	if strings.HasPrefix(strings.ToLower(text), "0x") {
		base = 0
	} else if len(text) == 4 {
		base = 16
	}
	n, e := strconv.ParseInt(text, base, 32)
	return int(n), e
}
func (p *Profile) ResolveDID(ctx context.Context, ecu EcuSpec, query string) (int, []map[string]any, error) {
	category, e := p.Category(ctx, ecu)
	if e != nil {
		return 0, nil, e
	}
	dids := profileMap(category["dids"])
	if number, e := profileHexKey(query); e == nil {
		if number < 0 || number > 65535 {
			return 0, nil, fmt.Errorf("DID out of range: %s", query)
		}
		return number, profileRows(dids[fmt.Sprintf("0x%04X", number)]), nil
	}
	exact, fuzzy := map[int][]map[string]any{}, map[int][]map[string]any{}
	for key, value := range dids {
		number, e := strconv.ParseInt(strings.TrimPrefix(strings.ToLower(key), "0x"), 16, 32)
		if e != nil {
			return 0, nil, fmt.Errorf("invalid DID key %q", key)
		}
		rows := profileRows(value)
		for _, row := range rows {
			name := profileText(row["name"])
			if strings.EqualFold(name, query) {
				exact[int(number)] = rows
			} else if strings.Contains(strings.ToLower(name), strings.ToLower(query)) {
				fuzzy[int(number)] = rows
			}
		}
	}
	matches := exact
	if len(matches) == 0 {
		matches = fuzzy
	}
	if len(matches) == 1 {
		for n, rows := range matches {
			return n, rows, nil
		}
	}
	if len(matches) == 0 {
		return 0, nil, fmt.Errorf("no DID matches %q", query)
	}
	ids := []int{}
	for id := range matches {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return 0, nil, fmt.Errorf("DID name %q maps to multiple DIDs: %X", query, ids)
}

// Commset reads one CommSet from the regional index.
func (p *Profile) Commset(ctx context.Context, id int) (map[string]any, error) {
	commsets, e := p.Section(ctx, "commsets")
	return profileMap(profileMap(commsets)[strconv.Itoa(id)]), e
}

// SessionControl reads the regional session lifecycle table.
func (p *Profile) SessionControl(ctx context.Context) (map[string]any, error) {
	value, e := p.Section(ctx, "session_control")
	return profileMap(value), e
}
func (p *Profile) SessionCommset(ctx context.Context, ecu EcuSpec) (int, map[string]any, error) {
	control, e := p.SessionControl(ctx)
	if e != nil {
		return 0, nil, e
	}
	row := profileMap(profileMap(control["per_category"])[strconv.Itoa(ecu.CategoryID)])
	ids := map[int]bool{}
	for _, key := range []string{"default_session", "extended_session", "keepalive"} {
		frame := profileMap(row[key])
		if frame["comm_set"] != nil {
			ids[profileInt(frame["comm_set"])] = true
		}
	}
	if len(ids) == 0 {
		return 0, nil, nil
	}
	if len(ids) > 1 {
		return 0, nil, fmt.Errorf("session_control category %d: expected one shared CommSet", ecu.CategoryID)
	}
	for id := range ids {
		row, e := p.Commset(ctx, id)
		if e != nil {
			return 0, nil, e
		}
		if row == nil {
			return 0, nil, fmt.Errorf("session_control category %d: CommSet %d is not present", ecu.CategoryID, id)
		}
		return id, row, nil
	}
	return 0, nil, nil
}
