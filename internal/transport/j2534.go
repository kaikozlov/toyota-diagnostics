package transport

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
	"unsafe"

	"github.com/kaikozlov/toyota-diagnostics/internal/appenv"
)

const jCAN uint32 = 5
const jExtended uint32 = 0x100

var jExports = []string{"PassThruOpen", "PassThruClose", "PassThruConnect", "PassThruDisconnect", "PassThruReadMsgs", "PassThruWriteMsgs", "PassThruStartMsgFilter", "PassThruStopMsgFilter", "PassThruReadVersion", "PassThruGetLastError"}

type J2534Options struct {
	Library, Device string
	Baud, Bus       int
}
type J2534Provider struct {
	Name    string `json:"name"`
	Library string `json:"library"`
	Source  string `json:"source"`
}
type J2534Status struct {
	Backend        string `json:"backend"`
	Mode           string `json:"mode"`
	Ready          bool   `json:"ready"`
	HardwareProbed bool   `json:"hardware_probed"`
	Library        string `json:"library,omitempty"`
	Device         string `json:"device_selector,omitempty"`
	Baud           int    `json:"baud"`
	Detail         string `json:"detail"`
}
type jMessage struct {
	Protocol, RxStatus, TxFlags, Timestamp, DataSize, ExtraDataIndex uint32
	Data                                                             [4128]byte
}
type jLibrary interface {
	Call(int, ...any) int32
	Close() error
}
type j2534CAN struct {
	mu                      sync.Mutex
	api                     jLibrary
	device, channel         uint32
	deviceOpen, channelOpen bool
	filters                 []uint32
	bus                     int
}

func J2534Providers(explicit string) []J2534Provider {
	provider := func(path, source string) J2534Provider {
		return J2534Provider{strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), path, source}
	}
	if explicit != "" {
		return []J2534Provider{provider(explicit, "explicit")}
	}
	var rows []J2534Provider
	if path := appenv.J2534Library(); path != "" {
		rows = append(rows, provider(path, "environment"))
	}
	rows = append(rows, jSystemProviders()...)
	seen := map[string]bool{}
	out := make([]J2534Provider, 0, len(rows))
	for _, p := range rows {
		key := filepath.Clean(p.Library)
		if runtime.GOOS == "windows" {
			key = strings.ToLower(key)
		}
		if !seen[key] {
			seen[key] = true
			out = append(out, p)
		}
	}
	return out
}
func jResolve(explicit string) (J2534Provider, error) {
	rows := J2534Providers(explicit)
	if len(rows) == 0 {
		return J2534Provider{}, errors.New("no J2534 library found; pass --j2534-library PATH, set TOYO_J2534_LIBRARY or TOYOTA_J2534_LIBRARY, install a Windows PassThruSupport.04.04 provider, or install libopenmvci")
	}
	return rows[0], nil
}
func ProbeJ2534(opts J2534Options) J2534Status {
	if opts.Baud == 0 {
		opts.Baud = 500000
	}
	s := J2534Status{Backend: "j2534", Mode: "j2534-raw-can", Library: opts.Library, Device: opts.Device, Baud: opts.Baud}
	p, err := jResolve(opts.Library)
	if err == nil {
		s.Library = p.Library
		var lib jLibrary
		lib, err = loadJ2534(p.Library)
		if err == nil {
			err = lib.Close()
		}
	}
	if err != nil {
		s.Detail = err.Error()
		return s
	}
	s.Ready = true
	s.Detail = "J2534 provider loaded; hardware is opened only by a live command"
	return s
}
func OpenJ2534(ctx context.Context, opts J2534Options) (CAN, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if opts.Baud == 0 {
		opts.Baud = 500000
	}
	if opts.Baud < 0 || uint64(opts.Baud) > uint64(^uint32(0)) {
		return nil, errors.New("J2534 baud must fit a positive 32-bit integer")
	}
	p, err := jResolve(opts.Library)
	if err != nil {
		return nil, err
	}
	lib, err := loadJ2534(p.Library)
	if err != nil {
		return nil, err
	}
	return openJ2534API(ctx, opts, lib)
}
func openJ2534API(ctx context.Context, opts J2534Options, lib jLibrary) (CAN, error) {
	a := &j2534CAN{api: lib, bus: opts.Bus}
	fail := func(err error) (CAN, error) { return nil, errors.Join(err, a.Close()) }
	if opts.Device == "" {
		opts.Device = appenv.J2534Device()
	}
	var selector []byte
	var ptr unsafe.Pointer
	if opts.Device != "" {
		if strings.ContainsRune(opts.Device, 0) {
			return fail(errors.New("J2534 device selector contains NUL"))
		}
		selector = append([]byte(opts.Device), 0)
		ptr = unsafe.Pointer(&selector[0])
	}
	status := lib.Call(0, ptr, unsafe.Pointer(&a.device))
	runtime.KeepAlive(selector)
	if err := a.check(status, "PassThruOpen"); err != nil {
		return fail(err)
	}
	a.deviceOpen = true
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	if err := a.check(lib.Call(2, uintptr(a.device), uintptr(jCAN), 0x800, uintptr(opts.Baud), unsafe.Pointer(&a.channel)), "PassThruConnect"); err != nil {
		return fail(err)
	}
	a.channelOpen = true
	for _, flags := range []uint32{0, jExtended} {
		mask := jMessage{Protocol: jCAN, TxFlags: flags, DataSize: 4}
		pattern := mask
		var id uint32
		status = lib.Call(6, uintptr(a.channel), 1, unsafe.Pointer(&mask), unsafe.Pointer(&pattern), 0, unsafe.Pointer(&id))
		runtime.KeepAlive(&mask)
		runtime.KeepAlive(&pattern)
		runtime.KeepAlive(&id)
		if err := a.check(status, "PassThruStartMsgFilter"); err != nil {
			return fail(err)
		}
		a.filters = append(a.filters, id)
	}
	return a, nil
}
func (a *j2534CAN) check(status int32, op string) error {
	if status == 0 {
		return nil
	}
	var buf [256]byte
	result := a.api.Call(9, unsafe.Pointer(&buf[0]))
	runtime.KeepAlive(&buf)
	detail := ""
	if result == 0 {
		detail = strings.TrimRight(string(buf[:]), "\x00")
	}
	return fmt.Errorf("J2534 %s failed status=%d (0x%08x): %s", op, status, uint32(status), detail)
}
func jCANMessage(f Frame) (jMessage, error) {
	m := jMessage{Protocol: jCAN}
	if f.Address > 0x1fffffff {
		return m, fmt.Errorf("CAN address does not fit 29 bits: %#x", f.Address)
	}
	if len(f.Data) > 8 {
		return m, errors.New("J2534 raw classic-CAN backend supports at most 8 data bytes")
	}
	if f.Address > 0x7ff {
		m.TxFlags = jExtended
	}
	m.DataSize = uint32(4 + len(f.Data))
	binary.BigEndian.PutUint32(m.Data[:4], f.Address)
	copy(m.Data[4:], f.Data)
	return m, nil
}
func (a *j2534CAN) Send(ctx context.Context, f Frame) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if !a.channelOpen {
		return errors.New("J2534 adapter is closed")
	}
	msg, err := jCANMessage(f)
	if err != nil {
		return err
	}
	timeout := uint32(1000)
	if deadline, ok := ctx.Deadline(); ok {
		left := time.Until(deadline)
		if left <= 0 {
			return context.DeadlineExceeded
		}
		if left < time.Second {
			timeout = uint32((left + time.Millisecond - 1) / time.Millisecond)
		}
	}
	count := uint32(1)
	status := a.api.Call(5, uintptr(a.channel), unsafe.Pointer(&msg), unsafe.Pointer(&count), uintptr(timeout))
	runtime.KeepAlive(&msg)
	runtime.KeepAlive(&count)
	if err := a.check(status, "PassThruWriteMsgs"); err != nil {
		return err
	}
	if count != 1 {
		return fmt.Errorf("J2534 PassThruWriteMsgs reported %d of 1 messages written", count)
	}
	return nil
}
func (a *j2534CAN) Receive(ctx context.Context) ([]Frame, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !a.channelOpen {
		return nil, errors.New("J2534 adapter is closed")
	}
	var messages [64]jMessage
	count := uint32(len(messages))
	status := a.api.Call(4, uintptr(a.channel), unsafe.Pointer(&messages[0]), unsafe.Pointer(&count), 0)
	runtime.KeepAlive(&messages)
	runtime.KeepAlive(&count)
	switch status {
	case 0, 9, 16, -6, -7:
	default:
		return nil, a.check(status, "PassThruReadMsgs")
	}
	if count > uint32(len(messages)) {
		return nil, fmt.Errorf("J2534 provider returned invalid message count %d", count)
	}
	frames := make([]Frame, 0, count)
	for _, m := range messages[:count] {
		if m.Protocol != jCAN || m.RxStatus&1 != 0 || m.DataSize < 4 {
			continue
		}
		if m.DataSize > 12 {
			return nil, fmt.Errorf("J2534 raw-CAN message has invalid data size %d", m.DataSize)
		}
		frames = append(frames, Frame{Address: binary.BigEndian.Uint32(m.Data[:4]) & 0x1fffffff, Data: append([]byte(nil), m.Data[4:m.DataSize]...), Bus: a.bus})
	}
	return frames, nil
}
func (a *j2534CAN) Clear(ctx context.Context) error {
	for i := 0; i < 32; i++ {
		frames, err := a.Receive(ctx)
		if err != nil {
			return err
		}
		if len(frames) == 0 {
			return nil
		}
	}
	return nil
}
func (a *j2534CAN) Close() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.api == nil {
		return nil
	}
	var errs []error
	for _, id := range a.filters {
		errs = append(errs, a.check(a.api.Call(7, uintptr(a.channel), uintptr(id)), "PassThruStopMsgFilter"))
	}
	a.filters = nil
	if a.channelOpen {
		errs = append(errs, a.check(a.api.Call(3, uintptr(a.channel)), "PassThruDisconnect"))
		a.channelOpen = false
	}
	if a.deviceOpen {
		errs = append(errs, a.check(a.api.Call(1, uintptr(a.device)), "PassThruClose"))
		a.deviceOpen = false
	}
	errs = append(errs, a.api.Close())
	a.api = nil
	return errors.Join(errs...)
}

func jArgument(value any) uintptr {
	switch v := value.(type) {
	case uintptr:
		return v
	case int:
		return uintptr(v)
	case uint32:
		return uintptr(v)
	case unsafe.Pointer:
		return uintptr(v)
	default:
		panic("invalid J2534 ABI argument")
	}
}
