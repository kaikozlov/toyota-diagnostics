package catalog

import (
	"context"
	"fmt"
)

// A root pattern is a presentation of the same pattern definition used by an
// operation. No second fact table or cross-kind cache key is necessary.
func (d *DB) legacyOperationPatternView(ctx context.Context, id int64) (any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid legacy operation pattern reference")
	}
	value, err := d.legacyOperationPattern(ctx, id)
	if err != nil {
		return nil, err
	}
	value["kind"] = "active_test_pattern"
	return value, nil
}
