package diagnostic

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
)

type RequestClient interface {
	Request(context.Context, byte, []byte) ([]byte, error)
}
type KeepaliveSpec struct {
	Kind     string
	Interval time.Duration
	DID      uint16
}
type SessionLifecycle struct {
	Generation                      string
	DefaultSession, ExtendedSession byte
	EnterSequence                   [][]byte
	ReturnDefault                   []byte
	Keepalive                       *KeepaliveSpec
}
type Session struct {
	Client        RequestClient
	Lifecycle     *SessionLifecycle
	ActiveSession byte
	Extended      bool
	CleanupErrors []string
}

func NewSession(client RequestClient, lifecycle *SessionLifecycle) *Session {
	return &Session{Client: client, Lifecycle: lifecycle}
}
func diagInt(v any) (int, error) {
	switch n := v.(type) {
	case int:
		return n, nil
	case int64:
		return int(n), nil
	case uint64:
		return int(n), nil
	case float64:
		if n == float64(int(n)) {
			return int(n), nil
		}
	case json.Number:
		i, e := strconv.ParseInt(string(n), 0, 64)
		return int(i), e
	case string:
		i, e := strconv.ParseInt(strings.TrimSpace(n), 0, 64)
		return int(i), e
	}
	return 0, fmt.Errorf("expected integer, got %v", v)
}
func diagNum(v any) int            { n, _ := diagInt(v); return n }
func diagMap(v any) map[string]any { m, _ := v.(map[string]any); return m }
func diagString(v any) string      { s, _ := v.(string); return s }
func diagBytes(v any) ([]byte, error) {
	if b, ok := v.([]byte); ok {
		return bytes.Clone(b), nil
	}
	s, ok := v.(string)
	if !ok {
		return nil, fmt.Errorf("expected hexadecimal bytes, got %v", v)
	}
	s = strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(s), " ", ""), "_", "")
	return hex.DecodeString(strings.TrimPrefix(s, "0x"))
}
func diagClone(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
func diagDSC(v any) ([]byte, error) {
	b, e := diagBytes(v)
	if e != nil {
		return nil, e
	}
	if len(b) != 2 || b[0] != 0x10 || b[1] == 0 {
		return nil, fmt.Errorf("not an exact 10 XX session request: %x", b)
	}
	return b, nil
}
func ParseLifecycle(raw map[string]any, categoryID int) (*SessionLifecycle, error) {
	if raw == nil {
		return nil, nil
	}
	raw = diagClone(raw)
	if category := diagMap(diagMap(raw["per_category"])[strconv.Itoa(categoryID)]); category != nil {
		_, newShape := category["session_executor_supported"]
		_, oldShape := category["lifecycle_supported"]
		if newShape || oldShape {
			supported, ok := category["session_executor_supported"]
			if !ok {
				supported = category["lifecycle_supported"]
			}
			if supported == false {
				return nil, nil
			}
			d1, e := diagDSC(sessionFrame(category["default_session"]))
			if e != nil {
				return nil, e
			}
			d2, e := diagDSC(sessionFrame(category["extended_session"]))
			if e != nil {
				return nil, e
			}
			for k, want := range map[string]byte{"default_session_value": d1[1], "extended_session_value": d2[1]} {
				if v := category[k]; v != nil {
					n, e := diagInt(v)
					if e != nil || n != int(want) {
						return nil, fmt.Errorf("%s disagrees with send bytes", k)
					}
				}
			}
			raw["default_session"] = int(d1[1])
			raw["extended_session"] = int(d2[1])
			raw["return_default"] = d1
			raw["enter_sequence"] = []any{d1, d2}
			raw["keepalive"] = category["keepalive"]
		}
	}
	d, e := diagInt(raw["default_session"])
	if e != nil || d <= 0 || d > 255 {
		return nil, fmt.Errorf("invalid default session")
	}
	x, e := diagInt(raw["extended_session"])
	if e != nil || x <= 0 || x > 255 {
		return nil, fmt.Errorf("invalid extended session")
	}
	ret, e := diagDSC(raw["return_default"])
	if e != nil {
		return nil, e
	}
	if ret[1] != byte(d) {
		return nil, fmt.Errorf("return_default disagrees with default session")
	}
	seq, ok := raw["enter_sequence"].([]any)
	if !ok {
		if raw["enter_extended"] == nil {
			return nil, fmt.Errorf("missing recovered D1/D2 sequence")
		}
		seq = []any{ret, raw["enter_extended"]}
	}
	if len(seq) != 2 {
		return nil, fmt.Errorf("entry must reproduce exactly D1 then D2")
	}
	a, e := diagDSC(seq[0])
	if e != nil {
		return nil, e
	}
	b, e := diagDSC(seq[1])
	if e != nil {
		return nil, e
	}
	if a[1] != byte(d) || b[1] != byte(x) {
		return nil, fmt.Errorf("entry must reproduce exactly D1 then D2")
	}
	l := &SessionLifecycle{Generation: diagString(raw["kind"]), DefaultSession: byte(d), ExtendedSession: byte(x), EnterSequence: [][]byte{a, b}, ReturnDefault: ret}
	if raw["keepalive"] != nil {
		spec := diagMap(raw["keepalive"])
		if spec != nil && spec["steps"] != nil {
			steps, ok := spec["steps"].([]any)
			if !ok || len(steps) != 1 || diagMap(steps[0]) == nil {
				return nil, fmt.Errorf("keepalive requires one exactly recovered step")
			}
			step := diagClone(diagMap(steps[0]))
			step["interval_s"] = spec["interval_s"]
			spec = step
		}
		if spec == nil {
			return nil, fmt.Errorf("keepalive must be an object")
		}
		seconds, e := strconv.ParseFloat(fmt.Sprint(spec["interval_s"]), 64)
		if e != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64)/float64(time.Second) {
			return nil, fmt.Errorf("invalid keepalive interval")
		}
		k := &KeepaliveSpec{Kind: diagString(spec["kind"]), Interval: time.Duration(seconds * float64(time.Second)), DID: 0xf186}
		switch k.Kind {
		case "tester_present":
		case "session_did_poll", "did_poll":
			if spec["did"] != nil {
				n, e := diagInt(spec["did"])
				if e != nil || n < 0 || n > 65535 {
					return nil, fmt.Errorf("invalid keepalive DID")
				}
				k.DID = uint16(n)
			}
			for key, sid := range map[string]byte{"request": 0x22, "positive_prefix": 0x62} {
				if spec[key] != nil {
					got, e := diagBytes(spec[key])
					if e != nil || !bytes.Equal(got, []byte{sid, byte(k.DID >> 8), byte(k.DID)}) {
						return nil, fmt.Errorf("keepalive %s disagrees with DID", key)
					}
				}
			}
		case "extended_session_refresh":
			q, e := diagDSC(spec["request"])
			if e != nil || q[1] != byte(x) {
				return nil, fmt.Errorf("keepalive request disagrees with extended session")
			}
		default:
			return nil, fmt.Errorf("unsupported keepalive kind %q", k.Kind)
		}
		l.Keepalive = k
	}
	return l, nil
}
func ReadDID(ctx context.Context, c RequestClient, did uint16) ([]byte, error) {
	r, e := c.Request(ctx, 0x22, []byte{byte(did >> 8), byte(did)})
	if e != nil {
		return nil, e
	}
	if len(r) < 2 || r[0] != byte(did>>8) || r[1] != byte(did) {
		return nil, fmt.Errorf("DID %04X response echo mismatch: %x", did, r)
	}
	return r[2:], nil
}
func WriteDID(ctx context.Context, c RequestClient, did uint16, value []byte) error {
	r, e := c.Request(ctx, 0x2e, append([]byte{byte(did >> 8), byte(did)}, value...))
	if e != nil {
		return e
	}
	if len(r) < 2 || r[0] != byte(did>>8) || r[1] != byte(did) {
		return fmt.Errorf("DID %04X write echo mismatch: %x", did, r)
	}
	return nil
}
func (s *Session) control(ctx context.Context, v byte) error {
	r, e := s.Client.Request(ctx, 0x10, []byte{v})
	if e != nil {
		return e
	}
	if len(r) == 0 || r[0] != v {
		return fmt.Errorf("session response mismatch: %x", r)
	}
	return nil
}
func (s *Session) EnterExtended(ctx context.Context) error {
	l := s.Lifecycle
	if l == nil {
		return fmt.Errorf("registry supplies no recovered session lifecycle")
	}
	if s.ActiveSession == l.ExtendedSession {
		return nil
	}
	if l.Keepalive != nil && l.Keepalive.Kind == "session_did_poll" {
		r, e := ReadDID(ctx, s.Client, l.Keepalive.DID)
		if e == nil && len(r) > 0 && r[0] == l.ExtendedSession {
			s.ActiveSession = l.ExtendedSession
			s.Extended = true
			return nil
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var negative *transport.NegativeResponseError
		if e != nil && !errors.Is(e, context.DeadlineExceeded) && !errors.As(e, &negative) {
			return e
		}
	}
	for _, q := range l.EnterSequence {
		if e := s.control(ctx, q[1]); e != nil {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			if ce := s.RestoreDefault(cleanup); ce != nil {
				s.CleanupErrors = append(s.CleanupErrors, ce.Error())
			}
			return e
		}
	}
	s.ActiveSession = l.ExtendedSession
	s.Extended = true
	return nil
}
func (s *Session) RestoreDefault(ctx context.Context) error {
	if s.Lifecycle == nil {
		return fmt.Errorf("registry supplies no recovered session lifecycle")
	}
	if e := s.control(ctx, s.Lifecycle.DefaultSession); e != nil {
		return e
	}
	s.ActiveSession = 0
	s.Extended = false
	return nil
}
func (s *Session) Keepalive(ctx context.Context) error {
	if s.Lifecycle == nil || s.Lifecycle.Keepalive == nil {
		return fmt.Errorf("registry supplies no recovered keepalive")
	}
	k := s.Lifecycle.Keepalive
	switch k.Kind {
	case "tester_present":
		r, e := s.Client.Request(ctx, 0x3e, []byte{0})
		if e != nil {
			return e
		}
		if len(r) == 0 || r[0] != 0 {
			return fmt.Errorf("tester present echo mismatch")
		}
		return nil
	case "extended_session_refresh":
		return s.control(ctx, s.Lifecycle.ExtendedSession)
	default:
		r, e := ReadDID(ctx, s.Client, k.DID)
		if e != nil {
			return e
		}
		if len(r) == 0 {
			return fmt.Errorf("keepalive DID returned no data")
		}
		if k.Kind == "session_did_poll" && s.ActiveSession != 0 && r[0] != s.ActiveSession {
			return fmt.Errorf("ECU reports session %02x, expected %02x", r[0], s.ActiveSession)
		}
		return nil
	}
}
func (s *Session) Close(ctx context.Context) error {
	if !s.Extended {
		return nil
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	e := s.RestoreDefault(cleanup)
	if e != nil {
		s.CleanupErrors = append(s.CleanupErrors, e.Error())
	}
	return e
}

// CommsetTimeouts accepts only recovered seconds, not raw Toyota CommSet units.
func CommsetTimeouts(sessionControl, operation map[string]any) (time.Duration, time.Duration, error) {
	timeout, pending := 350*time.Millisecond, 2*time.Second
	for _, source := range []map[string]any{sessionControl, operation} {
		if source["commset"] == nil {
			continue
		}
		row := diagMap(source["commset"])
		if row == nil {
			return 0, 0, fmt.Errorf("commset must be an object")
		}
		for key, value := range row {
			if key != "uds_timeout_s" && key != "response_pending_timeout_s" {
				return 0, 0, fmt.Errorf("unsupported commset key %s; raw Toyota timeout units must not be treated as seconds", key)
			}
			seconds, err := strconv.ParseFloat(fmt.Sprint(value), 64)
			if err != nil || seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds > float64(math.MaxInt64)/float64(time.Second) {
				return 0, 0, fmt.Errorf("%s must be positive finite seconds", key)
			}
			duration := time.Duration(seconds * float64(time.Second))
			if duration <= 0 {
				return 0, 0, fmt.Errorf("%s is below clock precision", key)
			}
			if key == "uds_timeout_s" {
				timeout = duration
			} else {
				pending = duration
			}
		}
	}
	return timeout, pending, nil
}

func sessionFrame(v any) any {
	m := diagMap(v)
	if m["send"] != nil {
		return m["send"]
	}
	return uniqueOperationFrame(m["frames"], "send")
}
