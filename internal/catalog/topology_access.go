package catalog

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// VehicleTopology materializes only the requested vehicle's CAN topology.
func (d *DB) VehicleTopology(ctx context.Context, region string, vehicleType int) ([]map[string]any, error) {
	pointer := "/regions/" + escapePointer(strings.ToUpper(region)) + "/can_topology/" + strconv.Itoa(vehicleType)
	value, err := d.sourceValue(ctx, "index.json", pointer)
	if err != nil {
		return nil, fmt.Errorf("vehicle %d CAN topology: %w", vehicleType, err)
	}
	value, err = d.expandMetadata(ctx, value, make(referenceStack))
	if err != nil {
		return nil, err
	}
	rows, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid vehicle CAN topology")
	}
	result := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		object, ok := row.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid vehicle CAN topology row")
		}
		result = append(result, object)
	}
	return result, nil
}
