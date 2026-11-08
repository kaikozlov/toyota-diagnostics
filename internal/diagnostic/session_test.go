package diagnostic

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"
)

type scriptedClient struct {
	calls [][]byte
	reply func([]byte) ([]byte, error)
}

func (c *scriptedClient) Request(ctx context.Context, sid byte, data []byte) ([]byte, error) {
	q := append([]byte{sid}, data...)
	c.calls = append(c.calls, q)
	if c.reply != nil {
		return c.reply(q)
	}
	return bytes.Clone(data), nil
}
func lifecycleMap() map[string]any {
	return map[string]any{"default_session": 1, "extended_session": 3, "enter_sequence": []any{"1001", "1003"}, "return_default": "1001"}
}
func TestLifecycleExactSequence(t *testing.T) {
	for _, seq := range [][]any{{"1003"}, {"1003", "1001"}, {"1001", "1002"}, {"1001", "1003", "1003"}} {
		m := lifecycleMap()
		m["enter_sequence"] = seq
		if _, e := ParseLifecycle(m, 0); e == nil {
			t.Fatalf("accepted %v", seq)
		}
	}
}
func TestSessionEntryAndCleanup(t *testing.T) {
	l, e := ParseLifecycle(lifecycleMap(), 0)
	if e != nil {
		t.Fatal(e)
	}
	c := &scriptedClient{}
	s := NewSession(c, l)
	ctx := context.Background()
	if len(c.calls) != 0 {
		t.Fatal("construction transmits")
	}
	if e = s.EnterExtended(ctx); e != nil {
		t.Fatal(e)
	}
	if e = s.Close(ctx); e != nil {
		t.Fatal(e)
	}
	want := [][]byte{{0x10, 1}, {0x10, 3}, {0x10, 1}}
	for i := range want {
		if !bytes.Equal(want[i], c.calls[i]) {
			t.Fatal(c.calls)
		}
	}
}
func TestSessionPollSkipsTransition(t *testing.T) {
	m := lifecycleMap()
	m["keepalive"] = map[string]any{"kind": "session_did_poll", "interval_s": 1, "did": "0xF186"}
	l, e := ParseLifecycle(m, 0)
	if e != nil {
		t.Fatal(e)
	}
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) {
		if q[0] == 0x22 {
			return []byte{0xf1, 0x86, 3}, nil
		}
		return q[1:], nil
	}}
	s := NewSession(c, l)
	if e = s.EnterExtended(context.Background()); e != nil {
		t.Fatal(e)
	}
	if len(c.calls) != 1 || !s.Extended {
		t.Fatal(c.calls)
	}
	if e = s.Close(context.Background()); e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(c.calls[1], []byte{0x10, 1}) {
		t.Fatal(c.calls)
	}
}
func TestFailedTransitionUndoesWithLiveContext(t *testing.T) {
	l, _ := ParseLifecycle(lifecycleMap(), 0)
	ctx, cancel := context.WithCancel(context.Background())
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) {
		if bytes.Equal(q, []byte{0x10, 3}) {
			cancel()
			return nil, errors.New("lost response")
		}
		return q[1:], nil
	}}
	s := NewSession(c, l)
	if e := s.EnterExtended(ctx); e == nil {
		t.Fatal("expected failure")
	}
	if len(c.calls) != 3 || !bytes.Equal(c.calls[2], []byte{0x10, 1}) {
		t.Fatal(c.calls)
	}
}
func TestKeepaliveMetadataDisagreement(t *testing.T) {
	m := lifecycleMap()
	m["keepalive"] = map[string]any{"kind": "did_poll", "interval_s": 1, "did": "0xD100", "request": "22f186"}
	if _, e := ParseLifecycle(m, 0); e == nil {
		t.Fatal("accepted mismatch")
	}
}
func TestCategoryLifecycle(t *testing.T) {
	m := map[string]any{"per_category": map[string]any{"22": map[string]any{"session_executor_supported": true, "default_session": map[string]any{"send": "1001"}, "extended_session": map[string]any{"send": "1004"}, "keepalive": map[string]any{"kind": "extended_session_refresh", "interval_s": 0.5, "request": "1004"}}}}
	l, e := ParseLifecycle(m, 22)
	if e != nil {
		t.Fatal(e)
	}
	if l.ExtendedSession != 4 || l.Keepalive.Interval != 500*time.Millisecond {
		t.Fatal(l)
	}
}

func TestCommsetTimeoutPrecedence(t *testing.T) {
	a, b, e := CommsetTimeouts(map[string]any{"commset": map[string]any{"uds_timeout_s": 0.6}}, map[string]any{"commset": map[string]any{"uds_timeout_s": 0.2, "response_pending_timeout_s": 3}})
	if e != nil || a != 200*time.Millisecond || b != 3*time.Second {
		t.Fatal(a, b, e)
	}
	for _, value := range []any{0, -1, "NaN", "+Inf"} {
		if _, _, e := CommsetTimeouts(nil, map[string]any{"commset": map[string]any{"uds_timeout_s": value}}); e == nil {
			t.Fatal(value)
		}
	}
	if _, _, e := CommsetTimeouts(nil, map[string]any{"commset": map[string]any{"receive_timeout": 1020}}); e == nil {
		t.Fatal("raw units accepted")
	}
}
func TestMalformedSessionPollDoesNotTransition(t *testing.T) {
	m := lifecycleMap()
	m["keepalive"] = map[string]any{"kind": "session_did_poll", "interval_s": 1}
	l, _ := ParseLifecycle(m, 0)
	c := &scriptedClient{reply: func(q []byte) ([]byte, error) { return []byte{0xaa}, nil }}
	s := NewSession(c, l)
	if e := s.EnterExtended(context.Background()); e == nil || len(c.calls) != 1 {
		t.Fatal(e, c.calls)
	}
}

func TestV4LifecycleFrames(t *testing.T) {
	category := map[string]any{"session_executor_supported": true, "default_session_value": 1, "extended_session_value": 3, "default_session": map[string]any{"frames": []any{map[string]any{"send": "1001"}}}, "extended_session": map[string]any{"frames": []any{map[string]any{"send": "1003"}}}, "keepalive": map[string]any{"interval_s": 2, "steps": []any{map[string]any{"kind": "session_did_poll", "did": "0xF186", "request": "22f186", "positive_prefix": "62f186"}}}}
	raw := map[string]any{"per_category": map[string]any{"498": category}}
	l, e := ParseLifecycle(raw, 498)
	if e != nil || l == nil || l.Keepalive.DID != 0xf186 {
		t.Fatal(l, e)
	}
}
