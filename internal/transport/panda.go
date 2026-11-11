package transport

import (
	"bufio"
	"context"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"

	"github.com/kaikozlov/toyota-diagnostics/internal/appenv"
	"path/filepath"
	"sync"
	"time"
)

//go:embed panda_adapter.py
var pandaAdapter string

type PandaOptions struct {
	Python          string
	OBDMultiplexing bool
	Passive         bool
}
type Panda struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	closed bool
}
type wireFrame struct {
	Address uint32 `json:"address"`
	Data    string `json:"data"`
	Bus     int    `json:"bus"`
}
type pandaReply struct {
	OK     bool        `json:"ok"`
	Error  string      `json:"error"`
	Frames []wireFrame `json:"frames"`
}

func pythonCommand(explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if v := appenv.Python(); v != "" {
		return v, nil
	}
	for _, p := range []string{"/usr/local/venv/bin/python", "/usr/local/venv/bin/python3"} {
		if s, e := os.Stat(p); e == nil && !s.IsDir() {
			return p, nil
		}
	}
	// Reuse the project's existing environment, never create another venv.
	if exe, e := os.Executable(); e == nil {
		for _, dir := range []string{filepath.Dir(exe), filepath.Dir(filepath.Dir(exe))} {
			p := filepath.Join(dir, ".venv", "bin", "python")
			if s, e := os.Stat(p); e == nil && !s.IsDir() {
				return p, nil
			}
		}
	}
	return exec.LookPath("python3")
}
func startPanda(ctx context.Context, opts PandaOptions) (*Panda, error) {
	python, e := pythonCommand(opts.Python)
	if e != nil {
		return nil, fmt.Errorf("Panda I/O requires upstream Panda's Python environment: %w", e)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// The hardware lifetime outlives an interrupted operation so session cleanup
	// can still send stop/return-control with its independent cleanup context.
	cmd := exec.Command(python, "-u", "-c", pandaAdapter)
	cmd.Env = os.Environ()
	if s, e := os.Stat("/data/openpilot"); e == nil && s.IsDir() {
		cmd.Env = append(cmd.Env, "PYTHONPATH=/data/openpilot:"+os.Getenv("PYTHONPATH"))
	}
	cmd.Stderr = os.Stderr
	in, e := cmd.StdinPipe()
	if e != nil {
		return nil, e
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		in.Close()
		return nil, e
	}
	if e := cmd.Start(); e != nil {
		in.Close()
		return nil, e
	}
	return &Panda{cmd: cmd, in: in, out: bufio.NewReader(out)}, nil
}
func (p *Panda) exchange(ctx context.Context, req any, dest any) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	// An interrupted operation must not kill the worker. Drain its one reply
	// before permitting cleanup's next request, keeping the stream synchronized.
	type reply struct {
		data json.RawMessage
		err  error
	}
	result := make(chan reply, 1)
	go func() {
		var data json.RawMessage
		err := p.exchangeLocked(ctx, req, &data)
		result <- reply{data, err}
	}()
	select {
	case response := <-result:
		if response.err != nil {
			return response.err
		}
		if dest != nil {
			return json.Unmarshal(response.data, dest)
		}
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *Panda) exchangeLocked(ctx context.Context, req any, dest any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return errors.New("Panda adapter closed")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	b, e := json.Marshal(req)
	if e != nil {
		return e
	}
	b = append(b, '\n')
	if _, e = p.in.Write(b); e != nil {
		return e
	}
	line, e := p.out.ReadBytes('\n')
	if e != nil {
		return fmt.Errorf("Panda I/O adapter: %w", e)
	}
	var status pandaReply
	if e = json.Unmarshal(line, &status); e != nil {
		return e
	}
	if !status.OK {
		return errors.New(status.Error)
	}
	if dest != nil {
		return json.Unmarshal(line, dest)
	}
	return nil
}
func OpenPanda(ctx context.Context, opts PandaOptions) (CAN, error) {
	p, e := startPanda(ctx, opts)
	if e != nil {
		return nil, e
	}
	if e = p.exchange(ctx, map[string]any{"op": "open", "obd_multiplexing": opts.OBDMultiplexing, "passive": opts.Passive}, nil); e != nil {
		p.Close()
		return nil, e
	}
	return p, nil
}
func PandaStatus(ctx context.Context, opts PandaOptions) (map[string]any, error) {
	p, e := startPanda(ctx, opts)
	if e != nil {
		return nil, e
	}
	defer p.Close()
	var reply map[string]any
	e = p.exchange(ctx, map[string]any{"op": "status", "obd_multiplexing": opts.OBDMultiplexing}, &reply)
	delete(reply, "ok")
	return reply, e
}
func (p *Panda) Send(ctx context.Context, f Frame) error {
	if f.Address > 0x1fffffff || len(f.Data) > 64 {
		return errors.New("invalid CAN frame")
	}
	return p.exchange(ctx, map[string]any{"op": "send", "address": f.Address, "data": hex.EncodeToString(f.Data), "bus": f.Bus}, nil)
}
func (p *Panda) Receive(ctx context.Context) ([]Frame, error) {
	var reply pandaReply
	if e := p.exchange(ctx, map[string]string{"op": "recv"}, &reply); e != nil {
		return nil, e
	}
	frames := make([]Frame, 0, len(reply.Frames))
	for _, f := range reply.Frames {
		data, e := hex.DecodeString(f.Data)
		if e != nil {
			return nil, e
		}
		frames = append(frames, Frame{f.Address, data, f.Bus})
	}
	return frames, nil
}
func (p *Panda) Clear(ctx context.Context) error {
	return p.exchange(ctx, map[string]string{"op": "clear"}, nil)
}
func (p *Panda) Close() error {
	// Bound teardown even if an earlier USB call has not returned.
	timer := time.AfterFunc(2*time.Second, func() { _ = p.cmd.Process.Kill() })
	defer timer.Stop()
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return nil
	}
	p.closed = true
	// EOF asks Python to release the hardware even following an earlier error.
	e := p.in.Close()
	wait := p.cmd.Wait()
	if e != nil {
		return e
	}
	return wait
}
