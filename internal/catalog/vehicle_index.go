package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// Reused installation definitions share a bounded cache. Caller-owned decoded
// trees are still independent because the central expander copies every value.
func (d *DB) vehicleIndexCached(ctx context.Context, id, kind int64, read func() (any, error)) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid vehicle index reference %d", id)
	}
	cacheable := id <= (math.MaxInt64-2)/3
	var key int64
	if cacheable {
		key = id*3 + kind
		if value, ok := d.vehicleIndexRecords.load(key); ok {
			return value, nil
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	value, err := read()
	if err != nil {
		return nil, err
	}
	if cacheable {
		raw, err := json.Marshal(value)
		if err != nil {
			return nil, err
		}
		d.vehicleIndexRecords.store(key, value, len(raw))
	}
	return value, nil
}

func (d *DB) vehicleDefinition(ctx context.Context, id int64) (any, error) {
	return d.vehicleIndexCached(ctx, id, 0, func() (any, error) {
		definitions, err := d.vehicleDefinitions(ctx, []int64{id})
		if err != nil {
			return nil, err
		}
		return definitions[id], nil
	})
}

// vehicleDefinitionMap restores a map of vehicle definition references, such
// as a region's vehicles, with two queries rather than two per vehicle.
func (d *DB) vehicleDefinitionMap(ctx context.Context, node map[string]any, active referenceStack) (map[string]any, bool, error) {
	const marker = "$vehicle_definition"
	if len(node) < 2 {
		return nil, false, nil
	}
	ids := make(map[string]int64, len(node))
	for key, child := range node {
		ref, ok := child.(map[string]any)
		if !ok || len(ref) != 1 {
			return nil, false, nil
		}
		value, ok := ref[marker]
		if !ok {
			return nil, false, nil
		}
		id, err := referenceID(value, marker)
		if err != nil {
			return nil, true, err
		}
		if active[referenceKey{marker, id}] {
			return nil, true, fmt.Errorf("cyclic %s reference %d", marker, id)
		}
		ids[key] = id
	}
	list := make([]int64, 0, len(ids))
	for _, id := range ids {
		list = append(list, id)
	}
	definitions, err := d.vehicleDefinitions(ctx, list)
	if err != nil {
		return nil, true, err
	}
	out := make(map[string]any, len(ids))
	for key, id := range ids {
		out[key] = definitions[id]
	}
	return out, true, nil
}

type vehicleName struct {
	vehicleType float64
	name        string
}

// vehicleNames reads the type and name of vehicle definitions by ID, over
// the ID range like vehicleDefinitions. Given types, only definitions of
// those types are read; otherwise every ID must exist.
func (d *DB) vehicleNames(ctx context.Context, ids, types []int64) (map[int64]vehicleName, error) {
	names := make(map[int64]vehicleName, len(ids))
	if len(ids) == 0 {
		return names, nil
	}
	wanted := make(map[int64]bool, len(ids))
	low, high := ids[0], ids[0]
	for _, id := range ids {
		wanted[id] = true
		low, high = min(low, id), max(high, id)
	}
	query := `SELECT v.id,v.vehicle_type,t.value FROM vehicle_definitions v JOIN text_values t ON t.id=v.name_id
   WHERE v.id BETWEEN ? AND ?`
	args := []any{low, high}
	if len(types) > 0 {
		query += ` AND v.vehicle_type IN (` + strings.Repeat(",?", len(types))[1:] + `)`
		for _, t := range types {
			args = append(args, t)
		}
	}
	rows, err := d.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, vehicleType int64
		var name string
		if err := rows.Scan(&id, &vehicleType, &name); err != nil {
			return nil, err
		}
		if wanted[id] {
			names[id] = vehicleName{float64(vehicleType), name}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(types) == 0 {
		for _, id := range ids {
			if _, ok := names[id]; !ok {
				return nil, fmt.Errorf("vehicle definition %d: %w", id, sql.ErrNoRows)
			}
		}
	}
	return names, nil
}

// vehicleDefinitions reads vehicle definitions by ID with their install set
// IDs. A region's definitions have neighbouring IDs, so both queries read the
// ID range and drop rows outside the set.
func (d *DB) vehicleDefinitions(ctx context.Context, ids []int64) (map[int64]any, error) {
	definitions := make(map[int64]any, len(ids))
	if len(ids) == 0 {
		return definitions, nil
	}
	wanted := make(map[int64]bool, len(ids))
	low, high := ids[0], ids[0]
	for _, id := range ids {
		wanted[id] = true
		low, high = min(low, id), max(high, id)
	}
	query, err := d.prepared(ctx, `SELECT v.id,v.vehicle_type,t.value,v.install_count
   FROM vehicle_definitions v JOIN text_values t ON t.id=v.name_id WHERE v.id BETWEEN ? AND ?`)
	if err != nil {
		return nil, err
	}
	rows, err := query.QueryContext(ctx, low, high)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	counts := make(map[int64]int64, len(ids))
	refs := make(map[int64][]any, len(ids))
	for rows.Next() {
		var id, vehicleType, count int64
		var name string
		if err := rows.Scan(&id, &vehicleType, &name, &count); err != nil {
			return nil, err
		}
		if !wanted[id] {
			continue
		}
		refs[id] = make([]any, 0)
		counts[id] = count
		definitions[id] = map[string]any{"vehicle_type": catalogNumber(vehicleType), "name": name}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, id := range ids {
		if definitions[id] == nil {
			return nil, fmt.Errorf("vehicle definition %d: %w", id, sql.ErrNoRows)
		}
	}
	query, err = d.prepared(ctx, `SELECT vehicle_id,position,install_set_id FROM vehicle_install_references
   WHERE vehicle_id BETWEEN ? AND ? ORDER BY vehicle_id,position`)
	if err != nil {
		return nil, err
	}
	rows, err = query.QueryContext(ctx, low, high)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id, position, setID int64
		if err := rows.Scan(&id, &position, &setID); err != nil {
			return nil, err
		}
		if !wanted[id] {
			continue
		}
		if position != int64(len(refs[id])) {
			return nil, fmt.Errorf("invalid vehicle install order")
		}
		refs[id] = append(refs[id], catalogNumber(setID))
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, definition := range definitions {
		if int64(len(refs[id])) != counts[id] {
			return nil, fmt.Errorf("missing vehicle install references")
		}
		definition.(map[string]any)["install_set_ids"] = refs[id]
	}
	return definitions, nil
}

func (d *DB) installSet(ctx context.Context, id int64) (any, error) {
	return d.vehicleIndexCached(ctx, id, 1, func() (any, error) {
		var count int64
		definition, err := d.prepared(ctx, "SELECT member_count FROM install_set_definitions WHERE id=?")
		if err != nil {
			return nil, err
		}
		if err := definition.QueryRowContext(ctx, id).Scan(&count); err != nil {
			return nil, fmt.Errorf("install set %d: %w", id, err)
		}
		query, err := d.prepared(ctx, `SELECT m.position,c.category_id,c.comm_set_id,c.frame_id,c.route_selector,c.has_route
   FROM install_set_members m JOIN vehicle_connections c ON c.id=m.connection_id
   WHERE m.definition_id=? ORDER BY m.position`)
		if err != nil {
			return nil, err
		}
		rows, err := query.QueryContext(ctx, id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		members := make([]any, 0)
		for rows.Next() {
			var position, category, comm, frame, selector, hasRoute int64
			if err := rows.Scan(&position, &category, &comm, &frame, &selector, &hasRoute); err != nil {
				return nil, err
			}
			if position != int64(len(members)) || (hasRoute != 0 && hasRoute != 1) {
				return nil, fmt.Errorf("invalid install set member")
			}
			var route any
			if hasRoute == 1 {
				route = fmt.Sprintf("%d:%d", category, selector)
			}
			members = append(members, map[string]any{"category_id": catalogNumber(category), "connection_comm_set_id": catalogNumber(comm),
				"connection_frame_id": catalogNumber(frame), "connection_route_selector": catalogNumber(selector), "route_key": route})
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if int64(len(members)) != count {
			return nil, fmt.Errorf("missing install set members")
		}
		return members, nil
	})
}

var vehicleRouteIntegerFields = []string{
	"address_extension", "change_comm_if_case", "functional_address", "functional_address_field",
	"legislated_request_address", "protocol_info_id", "request_address", "request_address_field",
	"request_mask", "response_mask", "route_selector",
}
var vehicleRouteNullableFields = []string{"functional_request_address", "physical_request_address", "physical_response_address"}

func (d *DB) vehicleRoute(ctx context.Context, id int64) (any, error) {
	return d.vehicleIndexCached(ctx, id, 2, func() (any, error) {
		values := make([]int64, len(vehicleRouteIntegerFields))
		nullable := make([]sql.NullInt64, len(vehicleRouteNullableFields))
		var controller sql.NullString
		var controllerID sql.NullInt64
		var semantics, kind string
		dest := make([]any, 0, 17)
		for i := range values {
			dest = append(dest, &values[i])
		}
		for i := range nullable {
			dest = append(dest, &nullable[i])
		}
		dest = append(dest, &controller, &controllerID, &semantics, &kind)
		route, err := d.prepared(ctx, `SELECT d.address_extension,d.change_comm_if_case,d.functional_address,d.functional_address_field,
   d.legislated_request_address,d.protocol_info_id,d.request_address,d.request_address_field,d.request_mask,d.response_mask,d.route_selector,
   d.functional_request_address,d.physical_request_address,d.physical_response_address,c.value,d.controller_id,s.value,k.value
   FROM vehicle_route_definitions d
   LEFT JOIN text_values c ON c.id=d.controller_id
   JOIN text_values s ON s.id=d.request_address_field_semantics_id
   JOIN text_values k ON k.id=d.transport_kind_id WHERE d.id=?`)
		if err != nil {
			return nil, err
		}
		if err := route.QueryRowContext(ctx, id).Scan(dest...); err != nil {
			return nil, fmt.Errorf("vehicle route %d: %w", id, err)
		}
		if controllerID.Valid != controller.Valid {
			return nil, fmt.Errorf("missing vehicle route controller text")
		}
		out := make(map[string]any, 17)
		for i, key := range vehicleRouteIntegerFields {
			out[key] = catalogNumber(values[i])
		}
		for i, key := range vehicleRouteNullableFields {
			if nullable[i].Valid {
				out[key] = catalogNumber(nullable[i].Int64)
			} else {
				out[key] = nil
			}
		}
		out["controller"] = nil
		if controller.Valid {
			out["controller"] = controller.String
		}
		out["request_address_field_semantics"] = semantics
		out["transport_kind"] = kind
		return out, nil
	})
}
