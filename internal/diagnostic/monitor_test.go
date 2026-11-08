package diagnostic

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestMonitorSignalAndChanged(t *testing.T) {
	values := []map[string]any{{"ecu": map[string]any{"key": "brake"}, "did": 4096, "signals": []map[string]any{{"name": "Speed", "value": 10, "raw": 100, "unit": "km/h"}}}}
	text, state := RenderMonitorTable("brake", 1, 0, values, false, nil)
	if !strings.Contains(text, "Speed") || !strings.Contains(text, "0x1000") {
		t.Fatal(text)
	}
	text, _ = RenderMonitorTable("brake", 2, 1, values, true, state)
	if !strings.Contains(text, "no changes") {
		t.Fatal(text)
	}
	raw := FlattenSignalRecord(map[string]any{"data_hex": "abcd"})
	if raw[0]["signal"] != "(raw)" {
		t.Fatal(raw)
	}
}
func TestMonitorCSVAndCancellation(t *testing.T) {
	var b bytes.Buffer
	read := func(context.Context) ([]map[string]any, error) {
		return []map[string]any{{"ecu": map[string]any{"key": "a"}, "did": 1, "data_hex": "aa"}}, nil
	}
	e := RunMonitor(context.Background(), &b, "a", read, MonitorOptions{Count: 2, CSV: true})
	if e != nil || len(strings.Split(strings.TrimSpace(b.String()), "\n")) != 3 {
		t.Fatal(b.String(), e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if e := RunMonitor(ctx, &b, "a", read, MonitorOptions{Interval: time.Hour}); e != context.Canceled {
		t.Fatal(e)
	}
}
