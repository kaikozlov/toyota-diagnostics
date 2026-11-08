package transport

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCAN struct {
	frames  []Frame
	sent    []Frame
	onSend  func(Frame) []Frame
	cleared int
}

func (f *fakeCAN) Send(ctx context.Context, x Frame) error {
	f.sent = append(f.sent, x)
	if f.onSend != nil {
		f.frames = append(f.frames, f.onSend(x)...)
	}
	return nil
}
func (f *fakeCAN) Receive(ctx context.Context) ([]Frame, error) {
	v := f.frames
	f.frames = nil
	return v, nil
}
func (f *fakeCAN) Clear(ctx context.Context) error { f.frames = nil; f.cleared++; return nil }
func (f *fakeCAN) Close() error                    { return nil }
func TestPhysicalAndExtended(t *testing.T) {
	for _, extended := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "extended"}[extended], func(t *testing.T) {
			tx, rx := byte(0x5f), byte(0)
			f := &fakeCAN{}
			c := &Client{CAN: f, TxID: 0x750, RxID: 0x760, Timeout: time.Second}
			if extended {
				c.TxExtension = &tx
				c.RxExtension = &rx
			}
			f.onSend = func(_ Frame) []Frame {
				data := []byte{4, 0x62, 0xf1, 0x86, 1, 0, 0, 0}
				other := []byte{0xf, 2, 0x7e, 0, 0, 0, 0, 0}
				if extended {
					data = append([]byte{0}, data[:7]...)
				}
				return []Frame{{0x760, other, 0}, {0x760, data, 1}, {0x760, data, 0}}
			}
			// Normal addressing's unrelated ID must be filtered, not malformed input
			// from the matching node.
			if !extended {
				f.onSend = func(_ Frame) []Frame {
					return []Frame{{0x758, []byte{2, 0x7e, 0}, 0}, {0x760, []byte{4, 0x62, 0xf1, 0x86, 1}, 0}}
				}
			}
			got, e := c.Raw(context.Background(), []byte{0x22, 0xf1, 0x86})
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(got, []byte{0x62, 0xf1, 0x86, 1}) {
				t.Fatalf("%x", got)
			}
			want := []byte{3, 0x22, 0xf1, 0x86, 0, 0, 0, 0}
			if extended {
				want = []byte{0x5f, 3, 0x22, 0xf1, 0x86, 0, 0, 0}
			}
			if !bytes.Equal(f.sent[0].Data, want) {
				t.Fatalf("tx %x", f.sent[0].Data)
			}
		})
	}
}
func TestReceiveMultiFrameAndPending(t *testing.T) {
	f := &fakeCAN{}
	c := &Client{CAN: f, TxID: 0x7e0, Timeout: time.Second}
	f.onSend = func(x Frame) []Frame {
		if x.Data[0] == 0x30 {
			return []Frame{{0x7e8, []byte{0x21, 6, 7, 8, 9, 10, 11, 12}, 0}}
		}
		return []Frame{{0x7e8, []byte{3, 0x7f, 0x22, 0x78}, 0}, {0x7e8, []byte{0x10, 13, 0x62, 1, 2, 3, 4, 5}, 0}}
	}
	got, e := c.Raw(context.Background(), []byte{0x22, 0xf1, 0x90})
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(got, []byte{0x62, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12}) {
		t.Fatalf("%x", got)
	}
	if f.sent[1].Data[0] != 0x30 {
		t.Fatal("no flow control")
	}
}
func TestTransmitMultiFrameBlockSize(t *testing.T) {
	f := &fakeCAN{}
	c := &Client{CAN: f, TxID: 0x7e0, RxID: 0x7e8, Timeout: time.Second}
	f.onSend = func(x Frame) []Frame {
		if x.Data[0] == 0x22 {
			return []Frame{{0x7e8, []byte{2, 0x6e, 0}, 0}}
		}
		return []Frame{{0x7e8, []byte{0x30, 1, 0}, 0}}
	}
	request := append([]byte{0x2e}, bytes.Repeat([]byte{7}, 19)...)
	_, e := c.Raw(context.Background(), request)
	if e != nil {
		t.Fatal(e)
	}
	if len(f.sent) != 3 || f.sent[0].Data[0] != 0x10 || f.sent[1].Data[0] != 0x21 || f.sent[2].Data[0] != 0x22 {
		t.Fatalf("%v", f.sent)
	}
}
func TestNegativeAndTimeout(t *testing.T) {
	f := &fakeCAN{onSend: func(Frame) []Frame { return []Frame{{0x7e8, []byte{3, 0x7f, 0x22, 0x31}, 0}} }}
	c := &Client{CAN: f, TxID: 0x7e0, Timeout: time.Millisecond * 5}
	_, e := c.Request(context.Background(), 0x22, nil)
	var negative *NegativeResponseError
	if !errors.As(e, &negative) || negative.Code != 0x31 {
		t.Fatalf("%v", e)
	}
	f.onSend = nil
	_, e = c.Raw(context.Background(), []byte{0x22})
	if !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("%v", e)
	}
}
func TestBadSequence(t *testing.T) {
	f := &fakeCAN{onSend: func(x Frame) []Frame {
		if x.Data[0] == 0x30 {
			return []Frame{{0x7e8, []byte{0x22, 1, 2, 3, 4, 5, 6, 7}, 0}}
		}
		return []Frame{{0x7e8, []byte{0x10, 9, 0x62, 1, 2, 3, 4, 5}, 0}}
	}}
	c := &Client{CAN: f, TxID: 0x7e0, Timeout: time.Second}
	if _, e := c.Raw(context.Background(), []byte{0x22}); e == nil {
		t.Fatal("accepted wrong sequence")
	}
}
