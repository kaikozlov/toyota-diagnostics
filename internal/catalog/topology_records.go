package catalog

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
)

func topologyNumber(v int64) json.Number { return json.Number(strconv.FormatInt(v, 10)) }

func (d *DB) canTopology(ctx context.Context, id int64) (any, error) {
	if value, ok := d.topologyRecords.load(id); ok {
		return value, nil
	}
	var network, vehicle int64
	var name string
	err := d.db.QueryRowContext(ctx, `SELECT o.network_id,v.vehicle_type,t.value
 FROM topology_vehicle_views o JOIN vehicle_definitions v ON v.id=o.vehicle_id
 JOIN text_values t ON t.id=v.name_id WHERE o.id=?`, id).Scan(&network, &vehicle, &name)
	if err != nil {
		return nil, fmt.Errorf("CAN topology %d: %w", id, err)
	}
	shared, err := d.topologyNetwork(ctx, network)
	if err != nil {
		return nil, err
	}
	value := make(map[string]any, len(shared)+2)
	for k, v := range shared {
		value[k] = v
	}
	value["vehicle_type"] = topologyNumber(vehicle)
	value["vehicle_name"] = name
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d.topologyRecords.store(id, value, len(raw))
	return value, nil
}
func (d *DB) topologyIDs(ctx context.Context, query string, id, count int64) ([]int64, error) {
	if count < 0 {
		return nil, fmt.Errorf("invalid topology count")
	}
	rows, err := d.db.QueryContext(ctx, query, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]int64, 0)
	for rows.Next() {
		var pos, value int64
		if err := rows.Scan(&pos, &value); err != nil {
			return nil, err
		}
		if pos != int64(len(result)) {
			return nil, fmt.Errorf("invalid topology association order")
		}
		result = append(result, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if int64(len(result)) != count {
		return nil, fmt.Errorf("missing topology associations")
	}
	return result, nil
}
func (d *DB) topologyNetwork(ctx context.Context, id int64) (map[string]any, error) {
	if value, ok := d.topologyRecords.load(-id); ok {
		return value.(map[string]any), nil
	}
	var car, options, count int64
	if err := d.db.QueryRowContext(ctx, "SELECT can_car_id,option_count,variant_count FROM topology_networks WHERE id=?", id).Scan(&car, &options, &count); err != nil {
		return nil, err
	}
	if car < 0 || car > 0xffffffff || options < 0 {
		return nil, fmt.Errorf("invalid CAN network values")
	}
	ids, err := d.topologyIDs(ctx, "SELECT position,variant_id FROM topology_network_variants WHERE network_id=? ORDER BY position", id, count)
	if err != nil {
		return nil, err
	}
	variants := make([]any, 0, len(ids))
	for _, variant := range ids {
		var groupCount, placementCount int64
		if err := d.db.QueryRowContext(ctx, "SELECT group_count,placement_count FROM topology_variants WHERE id=?", variant).Scan(&groupCount, &placementCount); err != nil {
			return nil, err
		}
		groups, err := d.topologyIDs(ctx, "SELECT position,group_id FROM topology_variant_groups WHERE variant_id=? ORDER BY position", variant, groupCount)
		if err != nil {
			return nil, err
		}
		groupValues := make([]any, 0, len(groups))
		for _, g := range groups {
			if g < 0 || g > 0xffffffff {
				return nil, fmt.Errorf("invalid CAN component group")
			}
			groupValues = append(groupValues, fmt.Sprintf("0x%08X", g))
		}
		places, err := d.topologyIDs(ctx, "SELECT position,placement_id FROM topology_variant_placements WHERE variant_id=? ORDER BY position", variant, placementCount)
		if err != nil {
			return nil, err
		}
		placements := make([]any, 0, len(places))
		for _, place := range places {
			value, err := d.topologyPlacement(ctx, place)
			if err != nil {
				return nil, err
			}
			placements = append(placements, value)
		}
		variants = append(variants, map[string]any{"component_groups": groupValues, "placements": placements})
	}
	value := map[string]any{"can_bus_car_id": fmt.Sprintf("0x%08X", car), "option_count": topologyNumber(options), "placement_variant_count": topologyNumber(count), "placement_variants": variants}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	d.topologyRecords.store(-id, value, len(raw))
	return value, nil
}
func (d *DB) topologyPlacement(ctx context.Context, id int64) (any, error) {
	var bus, component, count int64
	var busName, domain, junction string
	err := d.db.QueryRowContext(ctx, `SELECT p.bus_index,b.value,p.component_index,d.value,j.value,p.gateway_count
 FROM topology_placements p JOIN topology_buses a ON a.bus_index=p.bus_index JOIN text_values b ON b.id=a.name_id
 JOIN text_values d ON d.id=p.domain_id JOIN text_values j ON j.id=p.junction_id WHERE p.id=?`, id).Scan(&bus, &busName, &component, &domain, &junction, &count)
	if err != nil {
		return nil, err
	}
	if component < 0 || component > 255 || bus < 0 || count < 0 {
		return nil, fmt.Errorf("invalid CAN placement")
	}
	rows, err := d.db.QueryContext(ctx, `SELECT g.position,t.value FROM topology_gateways g JOIN text_values t ON t.id=g.name_id WHERE g.placement_id=? ORDER BY g.position`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	gateways := make([]any, 0)
	for rows.Next() {
		var position int64
		var name string
		if err := rows.Scan(&position, &name); err != nil {
			return nil, err
		}
		if position != int64(len(gateways)) {
			return nil, fmt.Errorf("invalid CAN gateway order")
		}
		gateways = append(gateways, name)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if int64(len(gateways)) != count {
		return nil, fmt.Errorf("missing CAN gateway")
	}
	return map[string]any{"bus_index": topologyNumber(bus), "bus_name": busName, "component_index": topologyNumber(component), "component_hex": fmt.Sprintf("0x%02X", component), "ecu_domain": domain, "junction_name": junction, "gateway_names": gateways}, nil
}
