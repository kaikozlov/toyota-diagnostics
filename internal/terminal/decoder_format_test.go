package terminal

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/kaikozlov/toyota-diagnostics/internal/catalog"
)

func TestSignalPresentationUsesDeclaredDecoder(t *testing.T) {
	for _, decoder := range []string{"p6-native-msb0-u32-v1", "future-decoder", ""} {
		t.Run(decoder, func(t *testing.T) {
			payload, err := json.Marshal(map[string]any{"name": "Signal", "decoder": decoder, "sign_mode": 2, "signed": true, "mul": 2, "div": 4, "offset": 3})
			if err != nil {
				t.Fatal(err)
			}
			output := Render(120, false, func(p Printer) {
				p.Item(catalog.ItemDetail{Item: catalog.Item{ID: 1, Kind: "did", Name: "Signal", Identifier: "0x1234", Region: "NA"}, Payload: payload})
			})
			if strings.Contains(output, "two's complement") || strings.Contains(output, "raw ×") {
				t.Fatalf("applied P5 semantics to %q: %s", decoder, output)
			}
			if !strings.Contains(output, "Sign mode") || !strings.Contains(output, "2") {
				t.Fatalf("missing sign mode: %s", output)
			}
			if decoder != "" && !strings.Contains(output, decoder) {
				t.Fatalf("missing decoder: %s", output)
			}
		})
	}
}
