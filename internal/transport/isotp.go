// Package transport carries raw CAN and diagnostic packets, independent of Toyota metadata.
package transport

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

type Frame struct {
	Address uint32
	Data    []byte
	Bus     int
}
type CAN interface {
	Send(context.Context, Frame) error
	Receive(context.Context) ([]Frame, error)
	Clear(context.Context) error
	Close() error
}

// Client owns one sequential ISO-TP conversation. Callers sharing CAN must
// serialize conversations, since Receive drains a physical receive queue.
type Client struct {
	CAN        CAN
	TxID, RxID uint32
	// RxIDSet distinguishes an explicit CAN ID zero from inferred addressing.
	RxIDSet                  bool
	Bus                      int
	TxExtension, RxExtension *byte
	Timeout, PendingTimeout  time.Duration
	mu                       sync.Mutex
	queue                    [][]byte
}

type NegativeResponseError struct{ Service, Code byte }

func (e *NegativeResponseError) Error() string {
	return fmt.Sprintf("UDS service 0x%02x rejected: NRC 0x%02x", e.Service, e.Code)
}

func (c *Client) send(ctx context.Context, data []byte) error {
	frame := make([]byte, 8)
	start := 0
	if c.TxExtension != nil {
		frame[0] = *c.TxExtension
		start = 1
	}
	if len(data) > 8-start {
		return errors.New("ISO-TP frame exceeds classic CAN capacity")
	}
	copy(frame[start:], data)
	return c.CAN.Send(ctx, Frame{c.TxID, frame, c.Bus})
}
func (c *Client) next(ctx context.Context) ([]byte, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if len(c.queue) > 0 {
			v := c.queue[0]
			c.queue = c.queue[1:]
			return v, nil
		}
		frames, err := c.CAN.Receive(ctx)
		if err != nil {
			return nil, err
		}
		for _, f := range frames {
			if f.Address != c.RxID || f.Bus != c.Bus || len(f.Data) == 0 {
				continue
			}
			d := f.Data
			ext := c.RxExtension
			if ext == nil {
				ext = c.TxExtension
			}
			if ext != nil {
				if d[0] != *ext {
					continue
				}
				d = d[1:]
			}
			if len(d) > 0 {
				c.queue = append(c.queue, append([]byte(nil), d...))
			}
		}
		if len(c.queue) == 0 {
			if err := sleep(ctx, time.Millisecond); err != nil {
				return nil, err
			}
		}
	}
}
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
func separation(v byte) (time.Duration, error) {
	if v <= 0x7f {
		return time.Duration(v) * time.Millisecond, nil
	}
	if v >= 0xf1 && v <= 0xf9 {
		return time.Duration(v-0xf0) * 100 * time.Microsecond, nil
	}
	return 0, fmt.Errorf("invalid ISO-TP separation time 0x%02x", v)
}
func (c *Client) flow(ctx context.Context) (int, time.Duration, error) {
	for waits := 0; waits < 10; waits++ {
		d, e := c.next(ctx)
		if e != nil {
			return 0, 0, e
		}
		if len(d) < 3 || d[0]>>4 != 3 {
			return 0, 0, errors.New("expected ISO-TP flow control")
		}
		switch d[0] & 15 {
		case 0:
			delay, e := separation(d[2])
			return int(d[1]), delay, e
		case 1:
			continue
		case 2:
			return 0, 0, errors.New("ISO-TP receiver overflow")
		default:
			return 0, 0, errors.New("invalid ISO-TP flow status")
		}
	}
	return 0, 0, errors.New("too many ISO-TP flow-control waits")
}
func (c *Client) transmit(ctx context.Context, request []byte) error {
	capacity := 7
	if c.TxExtension != nil {
		capacity--
	}
	if len(request) == 0 || len(request) > 4095 {
		return errors.New("ISO-TP request must contain 1..4095 bytes")
	}
	if len(request) <= capacity {
		return c.send(ctx, append([]byte{byte(len(request))}, request...))
	}
	first := capacity - 1
	d := append([]byte{0x10 | byte(len(request)>>8), byte(len(request))}, request[:first]...)
	if e := c.send(ctx, d); e != nil {
		return e
	}
	offset, seq := first, byte(1)
	for offset < len(request) {
		block, delay, e := c.flow(ctx)
		if e != nil {
			return e
		}
		for n := 0; offset < len(request) && (block == 0 || n < block); n++ {
			if e := sleep(ctx, delay); e != nil {
				return e
			}
			end := min(offset+capacity, len(request))
			if e := c.send(ctx, append([]byte{0x20 | seq}, request[offset:end]...)); e != nil {
				return e
			}
			offset = end
			seq = (seq + 1) & 15
		}
	}
	return nil
}
func (c *Client) receive(ctx context.Context) ([]byte, error) {
	d, e := c.next(ctx)
	if e != nil {
		return nil, e
	}
	switch d[0] >> 4 {
	case 0:
		n := int(d[0] & 15)
		if n == 0 || n > len(d)-1 {
			return nil, errors.New("invalid ISO-TP single-frame length")
		}
		return append([]byte(nil), d[1:1+n]...), nil
	case 1:
		if len(d) < 2 {
			return nil, errors.New("truncated ISO-TP first frame")
		}
		n := int(d[0]&15)<<8 | int(d[1])
		capacity := 7
		if c.RxExtension != nil || c.TxExtension != nil {
			capacity--
		}
		if n <= capacity {
			return nil, errors.New("invalid ISO-TP multi-frame length")
		}
		out := append([]byte(nil), d[2:]...)
		if e := c.send(ctx, []byte{0x30, 0, 0}); e != nil {
			return nil, e
		}
		seq := byte(1)
		for len(out) < n {
			d, e = c.next(ctx)
			if e != nil {
				return nil, e
			}
			if len(d) < 2 || d[0] != 0x20|seq {
				return nil, fmt.Errorf("ISO-TP consecutive-frame sequence mismatch: want %d", seq)
			}
			out = append(out, d[1:]...)
			seq = (seq + 1) & 15
		}
		return out[:n], nil
	default:
		return nil, fmt.Errorf("unexpected ISO-TP frame type %d", d[0]>>4)
	}
}
func (c *Client) Raw(ctx context.Context, request []byte) ([]byte, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.CAN == nil {
		return nil, errors.New("no CAN transport")
	}
	if c.RxID == 0 && !c.RxIDSet {
		if c.TxID <= 0x7ff {
			c.RxID = c.TxID + 8
		} else {
			c.RxID = (c.TxID & 0x1fff0000) | ((c.TxID & 255) << 8) | ((c.TxID >> 8) & 255)
		}
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = time.Second
	}
	pending := c.PendingTimeout
	if pending <= 0 {
		pending = 10 * time.Second
	}
	c.queue = nil
	if e := c.CAN.Clear(ctx); e != nil {
		return nil, e
	}
	tx, cancel := context.WithTimeout(ctx, timeout)
	e := c.transmit(tx, request)
	cancel()
	if e != nil {
		return nil, e
	}
	for {
		rx, cancel := context.WithTimeout(ctx, timeout)
		reply, e := c.receive(rx)
		cancel()
		if e != nil {
			return nil, e
		}
		if len(reply) >= 3 && reply[0] == 0x7f && reply[1] == request[0] && reply[2] == 0x78 {
			timeout = pending
			continue
		}
		return reply, nil
	}
}
func (c *Client) Request(ctx context.Context, service byte, data []byte) ([]byte, error) {
	reply, e := c.Raw(ctx, append([]byte{service}, data...))
	if e != nil {
		return nil, e
	}
	if len(reply) >= 3 && reply[0] == 0x7f {
		return nil, &NegativeResponseError{reply[1], reply[2]}
	}
	if len(reply) == 0 || reply[0] != service+0x40 {
		return nil, fmt.Errorf("unexpected UDS response for service 0x%02x: %x", service, reply)
	}
	return reply[1:], nil
}
