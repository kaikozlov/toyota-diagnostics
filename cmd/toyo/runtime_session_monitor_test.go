package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
)

func udsPayload(f transport.Frame) []byte {
	if len(f.Data) == 0 {
		return nil
	}
	n := int(f.Data[0] & 15)
	if n == 0 || 1+n > len(f.Data) {
		return append([]byte(nil), f.Data...)
	}
	return append([]byte(nil), f.Data[1:1+n]...)
}

type sessionCAN struct {
	sent    [][]byte
	pending []transport.Frame
	reply   func([]byte) ([]byte, error)
}

func (f *sessionCAN) Send(ctx context.Context, x transport.Frame) error {
	uds := udsPayload(x)
	f.sent = append(f.sent, uds)
	if f.reply == nil {
		return nil
	}
	resp, err := f.reply(uds)
	if err != nil {
		return err
	}
	if resp == nil {
		return nil
	}
	data := make([]byte, 8)
	data[0] = byte(len(resp))
	copy(data[1:], resp)
	f.pending = append(f.pending, transport.Frame{Address: x.Address + 8, Data: data, Bus: x.Bus})
	return nil
}
func (f *sessionCAN) Receive(context.Context) ([]transport.Frame, error) {
	v := f.pending
	f.pending = nil
	return v, nil
}
func (f *sessionCAN) Clear(context.Context) error { f.pending = nil; return nil }
func (f *sessionCAN) Close() error                { return nil }

func positiveUDS(req []byte) ([]byte, error) {
	switch req[0] {
	case 0x10:
		return []byte{0x50, req[1]}, nil
	case 0x22:
		out := append([]byte{0x62}, req[1:]...)
		return append(out, 0x00), nil
	case 0x3e:
		return []byte{0x7e, 0x00}, nil
	default:
		return nil, errors.New("unexpected SID")
	}
}

func samplingApp(t *testing.T, lifecycle map[string]any, extra ...diagnostic.EcuSpec) *runtimeApp {
	t.Helper()
	r := readApp(t, map[string]any{"dids": map[string]any{"0x1601": []any{map[string]any{"name": "LTA Control", "decoder": "p5-linear-msb0-v1", "bit_start": 0, "bit_end": 7}}}})
	r.profile.Vehicle = map[string]any{"vehicle_type": 1}
	r.profile.ECUs[0].Address = 0x7e0
	r.profile.ECUs[0].RouteResolved = true
	if lifecycle != nil {
		r.profile.Regional["session_control"] = lifecycle
	}
	r.profile.ECUs = append(r.profile.ECUs, extra...)
	r.opt.values["count"] = []string{"1"}
	r.opt.values["interval"] = []string{"0"}
	r.opt.flags["no-clear"] = true
	return r
}

func defaultLifecycle(keepalive map[string]any) map[string]any {
	m := map[string]any{"default_session": 1, "extended_session": 3, "enter_sequence": []any{"1001", "1003"}, "return_default": "1001"}
	if keepalive != nil {
		m["keepalive"] = keepalive
	}
	return m
}

func TestMonitorObserveWatchSessionLifecycle(t *testing.T) {
	brake := diagnostic.EcuSpec{Key: "brake", Name: "Brake", CategoryID: 405, Address: 0x7e1, RouteResolved: true}
	t.Run("monitor enters keepalive and restores", func(t *testing.T) {
		r := samplingApp(t, defaultLifecycle(nil))
		fake := &sessionCAN{reply: positiveUDS}
		r.transport = fake
		if _, e := capture(func() error { return r.monitorCommand([]string{"monitor", "eps", "0x1601"}) }); e != nil {
			t.Fatal(e)
		}
		want := [][]byte{{0x10, 0x01}, {0x10, 0x03}, {0x22, 0x16, 0x01}, {0x10, 0x01}}
		if len(fake.sent) != len(want) {
			t.Fatalf("got %x", fake.sent)
		}
		for i := range want {
			if !bytes.Equal(fake.sent[i], want[i]) {
				t.Fatalf("step %d: got %x want %x (%x)", i, fake.sent[i], want[i], fake.sent)
			}
		}
	})
	t.Run("did watch uses the same session path", func(t *testing.T) {
		r := samplingApp(t, defaultLifecycle(nil))
		fake := &sessionCAN{reply: positiveUDS}
		r.transport = fake
		if _, e := capture(func() error { return r.didCommand([]string{"did", "watch", "eps", "0x1601"}) }); e != nil {
			t.Fatal(e)
		}
		if len(fake.sent) != 4 || !bytes.Equal(fake.sent[0], []byte{0x10, 0x01}) || !bytes.Equal(fake.sent[1], []byte{0x10, 0x03}) || !bytes.Equal(fake.sent[2], []byte{0x22, 0x16, 0x01}) || !bytes.Equal(fake.sent[3], []byte{0x10, 0x01}) {
			t.Fatalf("%x", fake.sent)
		}
	})
	t.Run("observe sessions per ECU and restores reverse", func(t *testing.T) {
		r := samplingApp(t, defaultLifecycle(nil), brake)
		fake := &sessionCAN{reply: positiveUDS}
		r.transport = fake
		if _, e := capture(func() error { return r.monitorCommand([]string{"observe", "eps:0x1601", "brake:0x1601"}) }); e != nil {
			t.Fatal(e)
		}
		want := [][]byte{
			{0x10, 0x01}, {0x10, 0x03},
			{0x10, 0x01}, {0x10, 0x03},
			{0x22, 0x16, 0x01}, {0x22, 0x16, 0x01},
			{0x10, 0x01}, {0x10, 0x01},
		}
		if len(fake.sent) != len(want) {
			t.Fatalf("got %x", fake.sent)
		}
		for i := range want {
			if !bytes.Equal(fake.sent[i], want[i]) {
				t.Fatalf("step %d: got %x want %x (%x)", i, fake.sent[i], want[i], fake.sent)
			}
		}
	})
	t.Run("absent lifecycle does not invent session traffic", func(t *testing.T) {
		r := samplingApp(t, nil)
		fake := &sessionCAN{reply: positiveUDS}
		r.transport = fake
		if _, e := capture(func() error { return r.monitorCommand([]string{"monitor", "eps", "0x1601"}) }); e != nil {
			t.Fatal(e)
		}
		if len(fake.sent) != 1 || !bytes.Equal(fake.sent[0], []byte{0x22, 0x16, 0x01}) {
			t.Fatalf("unexpected session traffic: %x", fake.sent)
		}
	})
	t.Run("keepalive between samples", func(t *testing.T) {
		r := samplingApp(t, defaultLifecycle(map[string]any{"kind": "tester_present", "interval_s": 0.01}))
		r.opt.values["count"] = []string{"2"}
		r.opt.values["interval"] = []string{"0.03"}
		fake := &sessionCAN{reply: positiveUDS}
		r.transport = fake
		if _, e := capture(func() error { return r.monitorCommand([]string{"monitor", "eps", "0x1601"}) }); e != nil {
			t.Fatal(e)
		}
		want := [][]byte{{0x10, 0x01}, {0x10, 0x03}, {0x22, 0x16, 0x01}, {0x3e, 0x00}, {0x22, 0x16, 0x01}, {0x10, 0x01}}
		if len(fake.sent) != len(want) {
			t.Fatalf("got %x", fake.sent)
		}
		for i := range want {
			if !bytes.Equal(fake.sent[i], want[i]) {
				t.Fatalf("step %d: got %x want %x (%x)", i, fake.sent[i], want[i], fake.sent)
			}
		}
	})
	t.Run("restore after cancel", func(t *testing.T) {
		r := samplingApp(t, defaultLifecycle(nil))
		r.opt.values["count"] = []string{"0"}
		r.opt.values["interval"] = []string{"0.05"}
		ctx, cancel := context.WithCancel(context.Background())
		r.ctx = ctx
		reads := 0
		fake := &sessionCAN{reply: func(req []byte) ([]byte, error) {
			if req[0] == 0x22 {
				reads++
				if reads == 1 {
					time.AfterFunc(5*time.Millisecond, cancel)
				}
			}
			return positiveUDS(req)
		}}
		r.transport = fake
		_, e := capture(func() error { return r.monitorCommand([]string{"monitor", "eps", "0x1601"}) })
		if e == nil || !errors.Is(e, context.Canceled) {
			t.Fatalf("want canceled, got %v", e)
		}
		if len(fake.sent) < 4 || !bytes.Equal(fake.sent[0], []byte{0x10, 0x01}) || !bytes.Equal(fake.sent[1], []byte{0x10, 0x03}) || !bytes.Equal(fake.sent[len(fake.sent)-1], []byte{0x10, 0x01}) {
			t.Fatalf("missing enter/restore on cancel: %x", fake.sent)
		}
	})
	t.Run("failed enter still restores", func(t *testing.T) {
		r := samplingApp(t, defaultLifecycle(nil))
		fake := &sessionCAN{reply: func(req []byte) ([]byte, error) {
			if bytes.Equal(req, []byte{0x10, 0x03}) {
				return nil, errors.New("lost D2")
			}
			return positiveUDS(req)
		}}
		r.transport = fake
		if _, e := capture(func() error { return r.monitorCommand([]string{"monitor", "eps", "0x1601"}) }); e == nil || !strings.Contains(e.Error(), "lost D2") {
			t.Fatalf("%v", e)
		}
		want := [][]byte{{0x10, 0x01}, {0x10, 0x03}, {0x10, 0x01}}
		if len(fake.sent) != len(want) {
			t.Fatalf("got %x", fake.sent)
		}
		for i := range want {
			if !bytes.Equal(fake.sent[i], want[i]) {
				t.Fatalf("%x", fake.sent)
			}
		}
		for _, req := range fake.sent {
			if req[0] == 0x22 {
				t.Fatalf("read after failed enter: %x", fake.sent)
			}
		}
	})
}
