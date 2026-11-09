package catalog

import (
	"context"
	"encoding/json"
	"testing"
)

func TestUnsupportedReferencesFailClosed(t *testing.T) {
	d := emptyDB(t)
	for _, kind := range []string{"$native_record", "$plugin_query", "$physical", "$dtc_source", "$unknown"} {
		t.Run(kind, func(t *testing.T) {
			node := map[string]any{kind: json.Number("1")}
			if _, err := d.expandMetadata(context.Background(), node, make(referenceStack)); err == nil {
				t.Fatal("unsupported reference escaped")
			}
			if _, err := d.runtimeValue(context.Background(), node); err == nil {
				t.Fatal("unsupported runtime reference escaped")
			}
		})
	}
}
