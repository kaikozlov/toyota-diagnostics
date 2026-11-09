package catalog

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"math"
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
		var vehicleType, count int64
		var name string
		err := d.db.QueryRowContext(ctx, `SELECT v.vehicle_type,t.value,v.install_count
   FROM vehicle_definitions v JOIN text_values t ON t.id=v.name_id WHERE v.id=?`, id).Scan(&vehicleType, &name, &count)
		if err != nil {
			return nil, fmt.Errorf("vehicle definition %d: %w", id, err)
		}
		rows, err := d.db.QueryContext(ctx, "SELECT position,install_set_id FROM vehicle_install_references WHERE vehicle_id=? ORDER BY position", id)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		refs := make([]any, 0)
		for rows.Next() {
			var position, setID int64
			if err := rows.Scan(&position, &setID); err != nil {
				return nil, err
			}
			if position != int64(len(refs)) {
				return nil, fmt.Errorf("invalid vehicle install order")
			}
			refs = append(refs, catalogNumber(setID))
		}
		if err := rows.Err(); err != nil {
			return nil, err
		}
		if int64(len(refs)) != count {
			return nil, fmt.Errorf("missing vehicle install references")
		}
		return map[string]any{"vehicle_type": catalogNumber(vehicleType), "name": name, "install_set_ids": refs}, nil
	})
}

func (d *DB) installSet(ctx context.Context, id int64) (any, error) {
	return d.vehicleIndexCached(ctx, id, 1, func() (any, error) {
		var count int64
		if err := d.db.QueryRowContext(ctx, "SELECT member_count FROM install_set_definitions WHERE id=?", id).Scan(&count); err != nil {
			return nil, fmt.Errorf("install set %d: %w", id, err)
		}
		rows, err := d.db.QueryContext(ctx, `SELECT m.position,c.category_id,c.comm_set_id,c.frame_id,c.route_selector,c.has_route
   FROM install_set_members m JOIN vehicle_connections c ON c.id=m.connection_id
   WHERE m.definition_id=? ORDER BY m.position`, id)
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
		err := d.db.QueryRowContext(ctx, `SELECT d.address_extension,d.change_comm_if_case,d.functional_address,d.functional_address_field,
   d.legislated_request_address,d.protocol_info_id,d.request_address,d.request_address_field,d.request_mask,d.response_mask,d.route_selector,
   d.functional_request_address,d.physical_request_address,d.physical_response_address,c.value,d.controller_id,s.value,k.value
   FROM vehicle_route_definitions d
   LEFT JOIN text_values c ON c.id=d.controller_id
   JOIN text_values s ON s.id=d.request_address_field_semantics_id
   JOIN text_values k ON k.id=d.transport_kind_id WHERE d.id=?`, id).Scan(dest...)
		if err != nil {
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
