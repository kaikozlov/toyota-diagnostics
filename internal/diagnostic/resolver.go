package diagnostic

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

func resolveInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case byte:
		return int(n), nil
	case uint16:
		return int(n), nil
	case uint32:
		return int(n), nil
	case uint64:
		if n > uint64(^uint(0)>>1) {
			return 0, fmt.Errorf("integer out of range: %d", n)
		}
		return int(n), nil
	case json.Number:
		i, e := strconv.ParseInt(string(n), 10, 64)
		return int(i), e
	case float64:
		if n != float64(int(n)) {
			return 0, fmt.Errorf("noninteger %v", v)
		}
		return int(n), nil
	case string:
		base := 10
		if strings.HasPrefix(strings.ToLower(n), "0x") {
			base = 0
		}
		i, e := strconv.ParseInt(n, base, 64)
		return int(i), e
	default:
		return 0, fmt.Errorf("expected integer, got %v", v)
	}
}
func routeSelector(row map[string]any, canonical, legacy string) any {
	if v := row[canonical]; v != nil {
		return v
	}
	return row[legacy]
}
func resolveMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func resolveRows(v any) []map[string]any {
	if a, ok := v.([]map[string]any); ok {
		return a
	}
	out := []map[string]any{}
	if a, ok := v.([]any); ok {
		for _, x := range a {
			if m := resolveMap(x); m != nil {
				out = append(out, m)
			}
		}
	}
	return out
}
func resolveText(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func resolveHex(v any) ([]byte, error) {
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("expected hex string, got %v", v)
	}
	return hex.DecodeString(strings.Join(strings.Fields(s), ""))
}

type ToyotaRoute struct {
	CategoryID          int    `json:"category_id"`
	Name                string `json:"name"`
	Generation          int    `json:"generation"`
	PhaseType           int    `json:"phase_type"`
	RequestAddress      int    `json:"request_address"`
	RequestAddressField int    `json:"request_address_field"`
	AddressExtension    int    `json:"address_extension"`
	ProtocolInfoID      int    `json:"protocol_info_id"`
	FunctionalAddress   int    `json:"functional_address"`
	TransportKind       string `json:"transport_kind"`
	Controller          string `json:"controller,omitempty"`
}

func (r ToyotaRoute) UDSTransportSupported() bool {
	return r.TransportKind == "iso15765-phase-family" || r.TransportKind == "iso15765-29bit-normal-fixed" || r.TransportKind == "legacy-unclassified"
}
func RouteForCandidate(candidate map[string]any) (ToyotaRoute, error) {
	var r ToyotaRoute
	raw := resolveMap(candidate["transport_route"])
	if raw == nil {
		return r, fmt.Errorf("category %v has no Toyota transport_route", candidate["category_id"])
	}
	var err error
	for _, field := range []struct {
		value any
		dst   *int
	}{{candidate["category_id"], &r.CategoryID}, {candidate["generation"], &r.Generation}, {routeSelector(candidate, "connection_route_selector", "connection_phase_type"), &r.PhaseType}, {raw["request_address"], &r.RequestAddressField}, {raw["address_extension"], &r.AddressExtension}, {raw["protocol_info_id"], &r.ProtocolInfoID}, {raw["functional_address"], &r.FunctionalAddress}} {
		*field.dst, err = resolveInt(field.value)
		if err != nil {
			return r, fmt.Errorf("malformed Toyota route: %w", err)
		}
	}
	phase, err := resolveInt(routeSelector(raw, "route_selector", "phase_type"))
	if err != nil {
		return r, err
	}
	if phase != r.PhaseType {
		return r, fmt.Errorf("category %d install phase 0x%02X disagrees with route phase 0x%02X", r.CategoryID, r.PhaseType, phase)
	}
	r.RequestAddress = r.RequestAddressField
	if raw["physical_request_address"] != nil {
		r.RequestAddress, err = resolveInt(raw["physical_request_address"])
		if err != nil {
			return r, err
		}
	}
	if r.RequestAddress < 0 || r.RequestAddress > 0x1fffffff {
		return r, fmt.Errorf("category %d request address out of range", r.CategoryID)
	}
	if r.AddressExtension < 0 || r.AddressExtension > 255 {
		return r, fmt.Errorf("category %d Toyota address extension is not one byte: %d", r.CategoryID, r.AddressExtension)
	}
	r.Name = resolveText(candidate["name"])
	if r.Name == "" {
		r.Name = fmt.Sprintf("Category %d", r.CategoryID)
	}
	r.TransportKind = resolveText(raw["transport_kind"])
	if r.TransportKind == "" {
		r.TransportKind = "legacy-unclassified"
	}
	r.Controller = resolveText(raw["controller"])
	return r, nil
}
func ValidVIN(vin string) bool {
	if len(vin) != 17 || vin == strings.Repeat("0", 17) {
		return false
	}
	for _, c := range vin {
		if !(c >= '0' && c <= '9' || c >= 'A' && c <= 'Z') || strings.ContainsRune("IOQ", c) {
			return false
		}
	}
	return true
}
func VINDecisionMatches(row map[string]any, vin string, categoryID, phaseType int) (bool, error) {
	if !ValidVIN(vin) {
		return false, fmt.Errorf("invalid/unavailable VIN %q", vin)
	}
	category, e := resolveInt(row["category_id"])
	if e != nil {
		return false, e
	}
	phase, e := resolveInt(routeSelector(row, "route_selector", "phase_type"))
	if e != nil {
		return false, e
	}
	if category != categoryID || phase != phaseType {
		return false, nil
	}
	flags, e := resolveInt(row["flags"])
	if e != nil {
		return false, e
	}
	prefix, e := resolveHex(row["vin_prefix_hex"])
	if e != nil {
		return false, e
	}
	if len(prefix) != 11 {
		return false, fmt.Errorf("VIN decision prefix must be 11 bytes, got %d", len(prefix))
	}
	for i := 0; i < 11; i++ {
		if flags&(1<<uint(i)) == 0 && prefix[i] != vin[i] {
			return false, nil
		}
	}
	return true, nil
}
func AnalyzeSupportBitmap(base int, bitmap []byte, shift int) []int {
	out := []int{}
	for i, b := range bitmap {
		if i >= 32 {
			break
		}
		for bit := 0; bit < 8; bit++ {
			if b&(0x80>>uint(bit)) == 0 {
				continue
			}
			if shift != 0 {
				out = append(out, (base+((i*8+bit)<<uint(shift)))&0xffff)
			} else if i != 31 || bit != 7 {
				out = append(out, (base+1+i*8+bit)&0xffff)
			}
		}
	}
	return out
}
func AnalyzeP6SupportBitmap(base int, bitmap []byte) []int {
	out := []int{}
	for i, b := range bitmap {
		if i >= 32 {
			break
		}
		for bit := 0; bit < 8; bit++ {
			if b&(0x80>>uint(bit)) != 0 {
				out = append(out, (base+i*8+bit)&0xffff)
			}
		}
	}
	return out
}
func supportBitmapHas(bitmap []byte, index int) bool {
	return index >= 0 && index < 256 && index/8 < len(bitmap) && bitmap[index/8]&(0x80>>uint(index%8)) != 0
}

// SupportResolver implements the P5/P6 family-specific bitmap protocol.
// A nil StaticIDs uses wire queries; a nonnil empty slice is an empty static cache.
type SupportResolver struct {
	Client                   RequestClient
	Root                     int
	Mode                     string
	Routine                  bool
	Excluded                 map[int]bool
	SelectorMin, SelectorMax int
	StripRoutineInfo         bool
	StaticIDs                []int
	root                     []byte
	rootLoaded               bool
	selectors                map[int][]byte
}

func NewSupportResolver(client RequestClient, contract map[string]any, mode string, routine bool) (*SupportResolver, error) {
	if mode != "p5-standard" && mode != "p6-standard" {
		return nil, fmt.Errorf("Toyota support mode %s: exact support-list executor is not known", mode)
	}
	key := "did_root"
	sid := byte(0x22)
	positive := "62"
	if routine {
		key = "routine_root"
		sid = 0x31
		positive = "71"
	}
	row := resolveMap(contract[key])
	if row == nil {
		return nil, fmt.Errorf("support contract %s is missing", key)
	}
	request, e := resolveHex(row["request"])
	if e != nil {
		return nil, e
	}
	expected := 3
	if routine {
		expected = 4
	}
	if len(request) != expected || request[0] != sid || (routine && request[1] != 1) {
		return nil, fmt.Errorf("invalid %s request %X", key, request)
	}
	ps := strings.TrimPrefix(strings.ToLower(resolveText(row["positive_sid"])), "0x")
	if ps != positive {
		return nil, fmt.Errorf("invalid %s positive SID", key)
	}
	root := int(request[len(request)-2])<<8 | int(request[len(request)-1])
	rootKey := "root_base"
	if mode == "p5-standard" && routine {
		rootKey = "root_request_rid"
	}
	if row[rootKey] != nil {
		root, e = resolveInt(row[rootKey])
		if e != nil {
			return nil, e
		}
	}
	r := &SupportResolver{Client: client, Root: root, Mode: mode, Routine: routine, Excluded: map[int]bool{}, SelectorMin: 0x0200, SelectorMax: 0xdf00, selectors: map[int][]byte{}}
	excluded := row["selector_excluded"]
	if mode == "p5-standard" && !routine {
		r.Excluded[0xf300] = true
		r.Excluded[0xfd00] = true
		excluded = resolveMap(contract["standard_did"])["selector_excluded"]
	}
	if values, ok := excluded.([]any); ok {
		r.Excluded = map[int]bool{}
		for _, v := range values {
			id, e := resolveInt(v)
			if e != nil {
				return nil, e
			}
			r.Excluded[id] = true
		}
	}
	if routine && mode == "p5-standard" {
		if limits, ok := row["selector_range"].([]any); ok {
			if len(limits) != 2 {
				return nil, fmt.Errorf("P5 RID selector_range must have two entries")
			}
			r.SelectorMin, e = resolveInt(limits[0])
			if e != nil {
				return nil, e
			}
			r.SelectorMax, e = resolveInt(limits[1])
			if e != nil {
				return nil, e
			}
		}
	}
	return r, nil
}
func (r *SupportResolver) query(ctx context.Context, id int) ([]byte, error) {
	sid := byte(0x22)
	data := []byte{byte(id >> 8), byte(id)}
	echo := data
	if r.Routine {
		sid = 0x31
		data = append([]byte{1}, data...)
		echo = data
	}
	response, e := r.Client.Request(ctx, sid, data)
	if e != nil {
		return nil, e
	}
	if len(response) < len(echo) {
		return nil, fmt.Errorf("support 0x%04X response truncated", id)
	}
	for i, b := range echo {
		if response[i] != b {
			return nil, fmt.Errorf("support 0x%04X response echo mismatch", id)
		}
	}
	payload := response[len(echo):]
	if r.Routine && r.StripRoutineInfo {
		if len(payload) == 0 {
			return nil, fmt.Errorf("P5 RID 0x%04X response missing routine-info byte", id)
		}
		payload = payload[1:]
	}
	return append([]byte(nil), payload...), nil
}
func (r *SupportResolver) RootBitmap(ctx context.Context) ([]byte, error) {
	if r.StaticIDs != nil {
		return []byte{}, nil
	}
	if !r.rootLoaded {
		b, e := r.query(ctx, r.Root)
		if e != nil {
			return nil, e
		}
		r.root = b
		r.rootLoaded = true
	}
	return r.root, nil
}
func (r *SupportResolver) SupportedGroups(ctx context.Context) ([]int, error) {
	if r.StaticIDs != nil {
		out := []int{}
		for _, id := range r.StaticIDs {
			if id&255 == 0 {
				out = append(out, id)
			}
		}
		return out, nil
	}
	root, e := r.RootBitmap(ctx)
	if e != nil {
		return nil, e
	}
	if r.Mode == "p6-standard" {
		return AnalyzeP6SupportBitmap(r.Root, root), nil
	}
	return AnalyzeSupportBitmap(0, root, 8), nil
}
func (r *SupportResolver) selector(ctx context.Context, id int) ([]byte, error) {
	if r.selectors == nil {
		r.selectors = map[int][]byte{}
	}
	if data, ok := r.selectors[id]; ok {
		return data, nil
	}
	data, e := r.query(ctx, id)
	if e != nil {
		return nil, e
	}
	r.selectors[id] = data
	return data, nil
}
func (r *SupportResolver) Supports(ctx context.Context, id int) (bool, error) {
	if id < 0 || id > 65535 {
		return false, fmt.Errorf("identifier out of range: 0x%X", id)
	}
	if r.StaticIDs != nil {
		for _, v := range r.StaticIDs {
			if v == id {
				return true, nil
			}
		}
		return false, nil
	}
	root, e := r.RootBitmap(ctx)
	if e != nil {
		return false, e
	}
	selector := id & 0xff00
	index := (id & 255) - 1
	if r.Mode == "p6-standard" {
		if id >= r.Root && id <= r.Root+255 && supportBitmapHas(root, id-r.Root) {
			return true, nil
		}
		selector = r.Root + (id >> 8)
		index = id & 255
		if !supportBitmapHas(root, selector-r.Root) || r.Excluded[selector] {
			return false, nil
		}
	} else {
		if !supportBitmapHas(root, id>>8) {
			return false, nil
		}
		if id&255 == 0 {
			return true, nil
		}
		if r.Excluded[selector] || (r.Routine && (selector < r.SelectorMin || selector > r.SelectorMax)) {
			return false, nil
		}
	}
	bitmap, e := r.selector(ctx, selector)
	if e != nil {
		return false, e
	}
	return supportBitmapHas(bitmap, index), nil
}
func (r *SupportResolver) SupportedIDs(ctx context.Context) ([]int, error) {
	if r.StaticIDs != nil {
		return append([]int{}, r.StaticIDs...), nil
	}
	groups, e := r.SupportedGroups(ctx)
	if e != nil {
		return nil, e
	}
	out := []int{}
	seen := map[int]bool{}
	add := func(ids []int) {
		for _, id := range ids {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	if r.Mode == "p6-standard" {
		add(groups)
	}
	for _, g := range groups {
		if r.Mode != "p6-standard" {
			add([]int{g})
		}
		if r.Excluded[g] || (r.Mode == "p5-standard" && r.Routine && (g < r.SelectorMin || g > r.SelectorMax)) {
			continue
		}
		bitmap, e := r.selector(ctx, g)
		if e != nil {
			return nil, e
		}
		if r.Mode == "p6-standard" {
			add(AnalyzeP6SupportBitmap((g&255)<<8, bitmap))
		} else {
			add(AnalyzeSupportBitmap(g, bitmap, 0))
		}
	}
	return out, nil
}
func StaticRoutineIDs(activeTests, utilities []map[string]any) ([]int, error) {
	seen := map[int]bool{}
	for _, rows := range [][]map[string]any{activeTests, utilities} {
		for _, row := range rows {
			if row["routine_id"] != nil {
				id, e := resolveInt(row["routine_id"])
				if e != nil {
					return nil, e
				}
				seen[id] = true
			}
		}
	}
	out := make([]int, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Ints(out)
	return out, nil
}
