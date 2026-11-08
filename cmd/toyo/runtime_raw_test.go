package main

import (
	"bytes"
	"context"
	"errors"
	"github.com/kaikozlov/toyota-diagnostics/internal/diagnostic"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
	"testing"
	"time"
)

type rawFakeCAN struct {
	sent    []transport.Frame
	frames  []transport.Frame
	reply   []transport.Frame
	cleared bool
}

func (f *rawFakeCAN) Send(ctx context.Context, x transport.Frame) error {
	f.sent = append(f.sent, x)
	f.frames = append(f.frames, f.reply...)
	return nil
}
func (f *rawFakeCAN) Receive(ctx context.Context) ([]transport.Frame, error) {
	v := f.frames
	f.frames = nil
	return v, nil
}
func (f *rawFakeCAN) Clear(ctx context.Context) error { f.cleared = true; f.frames = nil; return nil }
func (f *rawFakeCAN) Close() error                    { return nil }
func TestFunctionalOBDMatchesCompleteExpectedFrames(t *testing.T) {
	f := &rawFakeCAN{reply: []transport.Frame{
		{Address: 0x7e8, Data: []byte{1, 0x44}, Bus: 1},
		{Address: 0x7e9, Data: []byte{1, 0x44}, Bus: 0},
		{Address: 0x7e8, Data: []byte{0x10, 0x44}, Bus: 0},
		{Address: 0x7e8, Data: []byte{7, 0x44}, Bus: 0},
		{Address: 0x7e8, Data: []byte{1, 0x43}, Bus: 0},
		{Address: 0x7e8, Data: []byte{1, 0x44}, Bus: 0},
	}}
	got, e := functionalOBD(context.Background(), f, 4, nil, map[uint32]bool{0x7e8: true}, 0, time.Second)
	if e != nil {
		t.Fatal(e)
	}
	if len(got) != 1 || !got[0x7e8] {
		t.Fatalf("%v", got)
	}
	if !f.cleared || len(f.sent) != 1 || !bytes.Equal(f.sent[0].Data, []byte{1, 4, 0, 0, 0, 0, 0, 0}) {
		t.Fatalf("%v", f)
	}
}
func TestFunctionalOBDDeadlineAndCancellation(t *testing.T) {
	f := &rawFakeCAN{}
	got, e := functionalOBD(context.Background(), f, 1, nil, map[uint32]bool{0x7e8: true}, 0, time.Millisecond)
	if e != nil || len(got) != 0 {
		t.Fatalf("%v %v", got, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, e = functionalOBD(ctx, f, 1, nil, map[uint32]bool{0x7e8: true}, 0, time.Second)
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("%v", e)
	}
}
func TestRawMutationsAndMalformedOptionsDoNotSend(t *testing.T) {
	for _, tc := range []struct {
		args   []string
		values map[string][]string
	}{
		{[]string{"uds", "raw", "0x7e0", "0x2e", "1234"}, nil},
		{[]string{"uds", "raw", "0x7e0", "0x22"}, map[string][]string{"rx-sub-address": {"256"}}},
		{[]string{"uds", "raw", "0x20000000", "0x22"}, nil},
		{[]string{"uds", "raw", "0x7e0", "0x22", "zz"}, nil},
	} {
		f := &rawFakeCAN{}
		r := &runtimeApp{ctx: context.Background(), transport: f, profile: &diagnostic.Profile{}, opt: runtimeOptions{values: tc.values, flags: map[string]bool{}}}
		if e := r.rawCommand(tc.args); e == nil {
			t.Fatalf("accepted %v", tc.args)
		}
		if len(f.sent) != 0 {
			t.Fatal("sent invalid request")
		}
	}
}
func TestRawExplicitZeroRXExtension(t *testing.T) {
	f := &rawFakeCAN{reply: []transport.Frame{{Address: 0x760, Data: []byte{0, 4, 0x62, 0xf1, 0x86, 1, 0, 0}, Bus: 0}}}
	r := &runtimeApp{ctx: context.Background(), transport: f, profile: &diagnostic.Profile{}, opt: runtimeOptions{values: map[string][]string{"sub-address": {"0x5f"}, "rx-address": {"0x760"}, "rx-sub-address": {"0"}}, flags: map[string]bool{}}}
	if e := r.rawCommand([]string{"uds", "raw", "0x750", "0x22", "f186"}); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(f.sent[0].Data, []byte{0x5f, 3, 0x22, 0xf1, 0x86, 0, 0, 0}) {
		t.Fatalf("%x", f.sent[0].Data)
	}
}
func TestSniffIsReceiveOnlyAndCountBounded(t *testing.T) {
	f := &rawFakeCAN{frames: []transport.Frame{{Address: 0x123, Data: []byte{1}, Bus: 1}, {Address: 0x123, Data: []byte{2}, Bus: 0}}}
	r := &runtimeApp{ctx: context.Background(), transport: f, profile: &diagnostic.Profile{}, opt: runtimeOptions{values: map[string][]string{"count": {"1"}, "duration": {"0"}}, flags: map[string]bool{}}}
	if e := r.canCommand([]string{"can", "sniff", "0x123"}); e != nil {
		t.Fatal(e)
	}
	if len(f.sent) != 0 || f.cleared {
		t.Fatal("passive sniff transmitted or cleared")
	}
}
