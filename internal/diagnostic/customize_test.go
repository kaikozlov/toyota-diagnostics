package diagnostic

import (
	"bytes"
	"context"
	"testing"
)

func TestCustomizeMerge(t *testing.T) {
	for _, tc := range []struct {
		a, b, mode int
		in, want   []byte
	}{{0, 7, 0, []byte{0xaa, 0x55}, []byte{1, 0x55}}, {2, 3, 0, []byte{0}, []byte{0x10}}, {9, 9, 1, []byte{0x40, 0}, []byte{0x40, 0x40}}} {
		item := map[string]any{"write_bit_start": tc.a, "write_bit_end": tc.b, "merge_mode": tc.mode}
		got, e := MergeCustomizeValue(tc.in, item, 1)
		if e != nil || !bytes.Equal(got, tc.want) {
			t.Fatalf("%x %v", got, e)
		}
	}
	if _, e := MergeCustomizeValue([]byte{0, 0}, map[string]any{"write_bit_start": 9, "write_bit_end": 9, "merge_mode": 1}, 1); e == nil {
		t.Fatal("unsupported field accepted")
	}
}
func TestCustomizeReadModifyWriteVerify(t *testing.T) {
	item := map[string]any{"write_did": 0x225a, "write_bit_start": 0, "write_bit_end": 0, "current_bit_start": 0, "current_bit_end": 0, "merge_mode": 0}
	value := []byte{0x12, 0x34}
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) {
		if q[0] == 0x2e {
			value = bytes.Clone(q[3:])
			return q[1:3], nil
		}
		return append([]byte{0x22, 0x5a}, value...), nil
	}}
	support := func(context.Context, int) (bool, error) { return true, nil }
	r, e := SetCustomize(context.Background(), c, item, 1, support)
	if e != nil || !r.Changed || !bytes.Equal(value, []byte{0x92, 0x34}) {
		t.Fatalf("%+v %v", r, e)
	}
	r, e = SetCustomize(context.Background(), c, item, 1, support)
	if e != nil || r.Changed {
		t.Fatal(r, e)
	}
}
func TestCustomizeReadbackMismatch(t *testing.T) {
	item := map[string]any{"write_did": 0x225a, "write_bit_start": 0, "write_bit_end": 0, "current_bit_start": 0, "current_bit_end": 0, "merge_mode": 0}
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) {
		if q[0] == 0x2e {
			return q[1:3], nil
		}
		return []byte{0x22, 0x5a, 0}, nil
	}}
	_, e := SetCustomize(context.Background(), c, item, 1, func(context.Context, int) (bool, error) { return true, nil })
	if e == nil {
		t.Fatal("readback mismatch accepted")
	}
}
