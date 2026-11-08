package diagnostic

import (
	"context"
	"fmt"
	"reflect"
	"testing"
)

type supportTestClient struct {
	calls []int
	data  map[int][]byte
}

func (c *supportTestClient) Request(ctx context.Context, sid byte, data []byte) ([]byte, error) {
	offset := 0
	if sid == 0x31 {
		offset = 1
	}
	id := int(data[offset])<<8 | int(data[offset+1])
	c.calls = append(c.calls, id)
	b, ok := c.data[id]
	if !ok {
		return nil, fmt.Errorf("unexpected %x", id)
	}
	return append(append([]byte{}, data...), b...), nil
}
func TestBitmapBoundaries(t *testing.T) {
	bitmap := make([]byte, 33)
	bitmap[0] = 0x81
	bitmap[31] = 1
	bitmap[32] = 255
	if got := AnalyzeSupportBitmap(0, bitmap, 8); !reflect.DeepEqual(got, []int{0, 0x700, 0xff00}) {
		t.Fatal(got)
	}
	if got := AnalyzeSupportBitmap(0x1000, bitmap, 0); !reflect.DeepEqual(got, []int{0x1001, 0x1008}) {
		t.Fatal(got)
	}
	if got := AnalyzeP6SupportBitmap(0x1000, bitmap); !reflect.DeepEqual(got, []int{0x1000, 0x1007, 0x10ff}) {
		t.Fatal(got)
	}
}
func TestP5SupportCache(t *testing.T) {
	root := make([]byte, 32)
	root[2] = 0x80
	client := &supportTestClient{data: map[int][]byte{0x101: root, 0x1000: {0x81}}}
	r, e := NewSupportResolver(client, map[string]any{"did_root": map[string]any{"request": "220101", "positive_sid": "0x62"}}, "p5-standard", false)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []int{0x1000, 0x1001, 0x1008} {
		ok, e := r.Supports(context.Background(), id)
		if !ok || e != nil {
			t.Fatal(id, ok, e)
		}
	}
	if len(client.calls) != 2 {
		t.Fatal(client.calls)
	}
	ok, e := r.Supports(context.Background(), 0x1002)
	if ok || e != nil {
		t.Fatal(ok, e)
	}
}
func TestP6SupportIndexIsNotP5(t *testing.T) {
	root := make([]byte, 32)
	root[2] = 0x80
	client := &supportTestClient{data: map[int][]byte{0xa100: root, 0xa110: {0x80}}}
	r, e := NewSupportResolver(client, map[string]any{"did_root": map[string]any{"request": "22a100", "positive_sid": "62"}}, "p6-standard", false)
	if e != nil {
		t.Fatal(e)
	}
	for _, id := range []int{0xa110, 0x1000} {
		ok, e := r.Supports(context.Background(), id)
		if !ok || e != nil {
			t.Fatal(id, ok, e)
		}
	}
	ok, e := r.Supports(context.Background(), 0x1001)
	if ok || e != nil {
		t.Fatal(ok, e)
	}
}
func TestVINWildcard(t *testing.T) {
	row := map[string]any{"category_id": 4, "phase_type": 5, "flags": 1, "vin_prefix_hex": "3032333435363738394142"}
	ok, e := VINDecisionMatches(row, "123456789ABCDEFGH", 4, 5)
	if !ok || e != nil {
		t.Fatal(ok, e)
	}
	if _, e := VINDecisionMatches(row, "bad", 4, 5); e == nil {
		t.Fatal("invalid VIN")
	}
}
