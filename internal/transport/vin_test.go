package transport

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestFunctionalVINInterleavedResponders(t *testing.T) {
	vin := []byte("JTDBR32E720012345")
	payload := append([]byte{0x62, 0xf1, 0x90}, vin...)
	f := &fakeCAN{}
	f.onSend = func(frame Frame) []Frame {
		switch frame.Address {
		case 0x7df:
			return []Frame{{0x7e8, append([]byte{0x10, 20}, payload[:6]...), 0}, {0x768, append([]byte{0x10, 20}, payload[:6]...), 0}, {0x7e8, []byte{0x21, 0, 0, 0, 0, 0, 0, 0}, 1}}
		case 0x18db33f1:
			return []Frame{{0x18daf110, append([]byte{0x10, 20}, payload[:6]...), 0}}
		case 0x7e0, 0x760, 0x18da10f1:
			rx := frame.Address + 8
			if frame.Address == 0x18da10f1 {
				rx = 0x18daf110
			}
			return []Frame{{rx, append([]byte{0x21}, payload[6:13]...), 0}, {rx, append([]byte{0x22}, payload[13:]...), 0}}
		}
		return nil
	}
	got, e := FunctionalVIN(context.Background(), f, 0, 10*time.Millisecond)
	if e != nil {
		t.Fatal(e)
	}
	if got.VIN != string(vin) || got.Address != 0x7e8 {
		t.Fatalf("%+v", got)
	}
	flow := map[uint32]bool{}
	for _, x := range f.sent {
		if x.Data[0] == 0x30 {
			flow[x.Address] = true
		}
	}
	for _, a := range []uint32{0x7e0, 0x760, 0x18da10f1} {
		if !flow[a] {
			t.Fatalf("missing FC to %x", a)
		}
	}
}
func TestFunctionalVINOBDFallback(t *testing.T) {
	vin := []byte("JTDBR32E720012345")
	f := &fakeCAN{}
	f.onSend = func(frame Frame) []Frame {
		if frame.Address == 0x7df && bytes.Equal(frame.Data[:3], []byte{2, 9, 2}) {
			p := append([]byte{0x49, 2, 1}, vin...)
			return []Frame{{0x7ea, append([]byte{0x10, 20}, p[:6]...), 0}, {0x7ea, append([]byte{0x21}, p[6:13]...), 0}, {0x7ea, append([]byte{0x22}, p[13:]...), 0}}
		}
		return nil
	}
	got, e := FunctionalVIN(context.Background(), f, 0, 5*time.Millisecond)
	if e != nil || got.VIN != string(vin) {
		t.Fatalf("%+v %v", got, e)
	}
}
func TestVINValidation(t *testing.T) {
	for _, s := range []string{"00000000000000000", "JTDBR32E72001234I", "short"} {
		if _, ok := vinString([]byte(s)); ok {
			t.Fatal(s)
		}
	}
	if v, ok := vinString(append([]byte{17}, []byte("JTDBR32E720012345\x00\x00")...)); !ok || v != "JTDBR32E720012345" {
		t.Fatalf("%q %v", v, ok)
	}
}
