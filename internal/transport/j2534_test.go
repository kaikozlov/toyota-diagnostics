package transport

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

type fakeJLibrary struct {
	calls      []int
	fail       map[int]int32
	closed     bool
	written    jMessage
	reads      []jMessage
	shortWrite bool
}

func (f *fakeJLibrary) Call(op int, args ...any) int32 {
	f.calls = append(f.calls, op)
	if code := f.fail[op]; code != 0 {
		return code
	}
	switch op {
	case 0:
		*(*uint32)(args[1].(unsafe.Pointer)) = 10
	case 2:
		*(*uint32)(args[4].(unsafe.Pointer)) = 20
	case 6:
		*(*uint32)(args[5].(unsafe.Pointer)) = uint32(len(f.calls))
	case 5:
		f.written = *(*jMessage)(args[1].(unsafe.Pointer))
		if f.shortWrite {
			*(*uint32)(args[2].(unsafe.Pointer)) = 0
		}
	case 4:
		out := unsafe.Slice((*jMessage)(args[1].(unsafe.Pointer)), 64)
		copy(out, f.reads)
		*(*uint32)(args[2].(unsafe.Pointer)) = uint32(len(f.reads))
		f.reads = nil
	}
	return 0
}
func (f *fakeJLibrary) Close() error { f.closed = true; return nil }
func TestJ2534FramesAndLifecycle(t *testing.T) {
	f := &fakeJLibrary{}
	can, err := openJ2534API(context.Background(), J2534Options{Baud: 500000, Bus: 2}, f)
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte{0x02, 0x10, 0x01}
	if err := can.Send(context.Background(), Frame{Address: 0x18da10f1, Data: payload, Bus: 2}); err != nil {
		t.Fatal(err)
	}
	if f.written.TxFlags != jExtended || !reflect.DeepEqual(f.written.Data[:7], []byte{0x18, 0xda, 0x10, 0xf1, 2, 0x10, 1}) {
		t.Fatalf("wrong wire frame: %+v", f.written)
	}
	echo := f.written
	echo.RxStatus = 1
	standard, _ := jCANMessage(Frame{Address: 0x7e8, Data: payload})
	f.reads = []jMessage{echo, standard, f.written}
	frames, err := can.Receive(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(frames) != 2 || frames[0].Address != 0x7e8 || frames[1].Address != 0x18da10f1 || frames[0].Bus != 2 {
		t.Fatalf("frames %+v", frames)
	}
	if err := can.Close(); err != nil {
		t.Fatal(err)
	}
	if !f.closed {
		t.Fatal("library not closed")
	}
	tail := f.calls[len(f.calls)-4:]
	if !reflect.DeepEqual(tail, []int{7, 7, 3, 1}) {
		t.Fatalf("cleanup: %v", f.calls)
	}
	before := len(f.calls)
	can.Close()
	if len(f.calls) != before {
		t.Fatal("close not idempotent")
	}
	if err := can.Send(context.Background(), Frame{}); err == nil {
		t.Fatal("send after close succeeded")
	}
}
func TestJ2534FailedSetupCleansOwnedResources(t *testing.T) {
	f := &fakeJLibrary{fail: map[int]int32{6: 1}}
	_, err := openJ2534API(context.Background(), J2534Options{Baud: 500000}, f)
	if err == nil || !strings.Contains(err.Error(), "PassThruStartMsgFilter") {
		t.Fatalf("error: %v", err)
	}
	if !f.closed || !reflect.DeepEqual(f.calls[len(f.calls)-2:], []int{3, 1}) {
		t.Fatalf("cleanup: %v", f.calls)
	}
}
func TestJ2534RejectsBadFramesAndShortWrites(t *testing.T) {
	for _, frame := range []Frame{{Address: 0x20000000}, {Data: make([]byte, 9)}} {
		if _, err := jCANMessage(frame); err == nil {
			t.Fatal("accepted invalid frame")
		}
	}
	f := &fakeJLibrary{shortWrite: true}
	c, err := openJ2534API(context.Background(), J2534Options{Baud: 500000}, f)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Send(context.Background(), Frame{Address: 0x700}); err == nil {
		t.Fatal("accepted short write")
	}
	f.reads = []jMessage{{Protocol: jCAN, DataSize: 4130}}
	if _, err := c.Receive(context.Background()); err == nil {
		t.Fatal("accepted malformed RX")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.Send(ctx, Frame{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancellation: %v", err)
	}
}
func TestJ2534ExplicitProviderAndEnvironment(t *testing.T) {
	t.Setenv("TOYO_J2534_LIBRARY", "environment.so")
	rows := J2534Providers("explicit.so")
	if len(rows) != 1 || rows[0].Library != "explicit.so" || rows[0].Source != "explicit" {
		t.Fatal(rows)
	}
}
