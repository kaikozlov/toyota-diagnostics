package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
)

func rawByte(s, name string, allowZero bool) (byte, error) {
	n, e := runtimeInt(s)
	if e != nil {
		return 0, e
	}
	if n < 0 || n > 255 || (!allowZero && n == 0) {
		return 0, fmt.Errorf("%s must be one byte", name)
	}
	return byte(n), nil
}
func rawAddress(s string) (uint32, error) {
	n, e := runtimeInt(s)
	if e != nil {
		return 0, e
	}
	if n < 0 || n > 0x1fffffff {
		return 0, fmt.Errorf("CAN address must fit 29 bits: %s", s)
	}
	return uint32(n), nil
}
func (r *runtimeApp) rawCommand(a []string) error {
	if len(a) < 4 || len(a) > 5 || a[1] != "raw" {
		return usagef("uds", "expected uds raw ECU SERVICE [DATA]")
	}
	service, e := rawByte(a[3], "service", false)
	if e != nil {
		return e
	}
	switch service {
	case 0x19, 0x22, 0x23, 0x24, 0x3e:
	default:
		if !r.opt.flag("force") {
			return fmt.Errorf("mutating service 0x%02X requires explicit --force acknowledgement", service)
		}
	}
	request := []byte{service}
	if s := r.opt.value("subfunction", ""); s != "" {
		b, e := rawByte(s, "subfunction", true)
		if e != nil {
			return e
		}
		request = append(request, b)
	}
	if len(a) == 5 {
		data, e := runtimeHex(a[4])
		if e != nil {
			return e
		}
		request = append(request, data...)
	}
	address, e := rawAddress(a[2])
	var extension *byte
	if e != nil {
		// A numeric value outside CAN's range is not an ECU-name search.
		if _, nerr := runtimeInt(a[2]); nerr == nil {
			return e
		}
		if e := r.ensureVehicle(); e != nil {
			return e
		}
		ecu, e := r.ecu(a[2])
		if e != nil {
			return e
		}
		if !ecu.RouteResolved {
			return fmt.Errorf("%s has no resolved route; select --vehicle TYPE", ecu.Key)
		}
		address = ecu.Address
		extension = ecu.SubAddr
	}
	if s := r.opt.value("sub-address", ""); s != "" {
		b, e := rawByte(s, "sub-address", true)
		if e != nil {
			return e
		}
		extension = &b
	}
	var rx uint32
	if s := r.opt.value("rx-address", ""); s != "" {
		rx, e = rawAddress(s)
		if e != nil {
			return e
		}
	}
	rxExtension := extension
	if s := r.opt.value("rx-sub-address", ""); s != "" {
		b, e := rawByte(s, "RX sub-address", true)
		if e != nil {
			return e
		}
		rxExtension = &b
	}
	if e := r.connect(false); e != nil {
		return e
	}
	client := &transport.Client{CAN: r.transport, TxID: address, RxID: rx, RxIDSet: r.opt.value("rx-address", "") != "", Bus: r.profile.Bus, TxExtension: extension, RxExtension: rxExtension, Timeout: 350 * time.Millisecond, PendingTimeout: 2 * time.Second}
	response, e := client.Raw(r.ctx, request)
	if e != nil {
		return e
	}
	if r.opt.json {
		return r.output(map[string]any{"request": hex.EncodeToString(request), "response": hex.EncodeToString(response)})
	}
	fmt.Printf("request:  %x\nresponse: %x\n", request, response)
	return nil
}
func (r *runtimeApp) canCommand(a []string) error {
	if len(a) < 2 {
		return usagef("can", "expected can topology or can sniff ADDRESS...")
	}
	switch a[1] {
	case "topology":
		if len(a) != 2 {
			return usagef("can", "expected can topology")
		}
		if r.profile.Vehicle == nil {
			return fmt.Errorf("CAN topology requires --vehicle TYPE")
		}
		rows, err := r.db.VehicleTopology(r.ctx, r.profile.Region, runtimeNumber(r.profile.Vehicle["vehicle_type"]))
		if err != nil {
			return err
		}
		if len(rows) == 0 {
			return fmt.Errorf("catalog does not carry GTS CAN topology for this vehicle")
		}
		if len(rows) == 1 {
			return r.output(rows[0])
		}
		return r.output(map[string]any{"rows": rows})
	case "sniff":
		if len(a) < 3 {
			return usagef("can", "expected can sniff ADDRESS...")
		}
		addresses := map[uint32]bool{}
		for _, s := range a[2:] {
			n, e := rawAddress(s)
			if e != nil {
				return e
			}
			addresses[n] = true
		}
		duration, e := r.opt.duration("duration", 5*time.Second)
		if e != nil {
			return e
		}
		count, e := runtimeInt(r.opt.value("count", "0"))
		if e != nil || count < 0 {
			return fmt.Errorf("--count must be >= 0")
		}
		if e := r.connect(true); e != nil {
			return e
		}
		started := time.Now()
		ctx := r.ctx
		cancel := func() {}
		if duration > 0 {
			ctx, cancel = context.WithTimeout(ctx, duration)
		}
		defer cancel()
		seen := 0
		for {
			frames, e := r.transport.Receive(ctx)
			if e != nil {
				if errors.Is(e, context.DeadlineExceeded) || errors.Is(e, context.Canceled) {
					return nil
				}
				return e
			}
			for _, f := range frames {
				if f.Bus != r.profile.Bus || !addresses[f.Address] {
					continue
				}
				seen++
				elapsed := time.Since(started).Seconds()
				if r.opt.json {
					if e := r.output(map[string]any{"sample": seen, "elapsed_s": elapsed, "bus": f.Bus, "address": f.Address, "data_hex": hex.EncodeToString(f.Data)}); e != nil {
						return e
					}
				} else {
					fmt.Printf("[%06d] +%9.3fs bus=%d addr=0x%X data=%x\n", seen, elapsed, f.Bus, f.Address, f.Data)
				}
				if count > 0 && seen >= count {
					return nil
				}
			}
			if e := rawWait(ctx, 10*time.Millisecond); e != nil {
				return nil
			}
		}
	default:
		return usagef("can", "unknown action %q", a[1])
	}
}
func rawWait(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
func (r *runtimeApp) functionalCommand(a []string) error {
	if len(a) < 3 || len(a) > 4 || a[1] != "obd" {
		return usagef("functional", "expected functional obd MODE [PAYLOAD]")
	}
	mode, e := rawByte(a[2], "mode", false)
	if e != nil {
		return e
	}
	switch mode {
	case 1, 2, 3, 5, 6, 7, 9, 10:
	default:
		if !r.opt.flag("force") {
			return fmt.Errorf("mutating OBD mode 0x%02X requires explicit --force acknowledgement", mode)
		}
	}
	var payload []byte
	if len(a) == 4 {
		payload, e = runtimeHex(a[3])
		if e != nil {
			return e
		}
	}
	if len(payload) > 6 {
		return fmt.Errorf("payload longer than six bytes does not fit the standard 8-byte functional frame")
	}
	window, e := r.opt.duration("window", time.Second)
	if e != nil {
		return e
	}
	if e := r.ensureVehicle(); e != nil {
		return e
	}
	responders := map[uint32]bool{}
	for _, ecu := range r.profile.ECUs {
		address := runtimeNumber(ecu.Route["legislated_request_address"])
		if address >= 0x7e0 && address <= 0x7e7 {
			responders[uint32(address+8)] = true
		}
	}
	if e := r.connect(false); e != nil {
		return e
	}
	positives, e := functionalOBD(r.ctx, r.transport, mode, payload, responders, r.profile.Bus, window)
	if e != nil {
		return e
	}
	expected, positive, missing := []uint32{}, []uint32{}, []uint32{}
	for address := range responders {
		expected = append(expected, address)
		if positives[address] {
			positive = append(positive, address)
		} else {
			missing = append(missing, address)
		}
	}
	for _, list := range [][]uint32{expected, positive, missing} {
		sort.Slice(list, func(i, j int) bool { return list[i] < list[j] })
	}
	if r.opt.json {
		return r.output(map[string]any{"mode": mode, "request_address": 0x7df, "expected": expected, "positive": positive, "missing": missing})
	}
	fmt.Printf("functional OBD Mode 0x%02x (0x7DF):\n", mode)
	for _, address := range expected {
		status := "NO POSITIVE RESPONSE"
		if positives[address] {
			status = fmt.Sprintf("positive 0x%02x", int(mode)+0x40)
		}
		fmt.Printf("  0x%03x: %s\n", address, status)
	}
	if len(missing) > 0 {
		parts := make([]string, len(missing))
		for i, a := range missing {
			parts[i] = fmt.Sprintf("0x%03x", a)
		}
		fmt.Printf("warning: no positive response from %s\n", strings.Join(parts, " "))
	}
	return nil
}
func functionalOBD(ctx context.Context, can transport.CAN, mode byte, payload []byte, responders map[uint32]bool, bus int, window time.Duration) (map[uint32]bool, error) {
	if len(payload) > 6 {
		return nil, fmt.Errorf("functional OBD payload exceeds six bytes")
	}
	if e := ctx.Err(); e != nil {
		return nil, e
	}
	request := make([]byte, 8)
	request[0] = byte(len(payload) + 1)
	request[1] = mode
	copy(request[2:], payload)
	if e := can.Clear(ctx); e != nil {
		return nil, e
	}
	if e := can.Send(ctx, transport.Frame{Address: 0x7df, Data: request, Bus: bus}); e != nil {
		return nil, e
	}
	positive := map[uint32]bool{}
	deadline, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	for {
		if e := deadline.Err(); e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return positive, nil
		}
		frames, e := can.Receive(deadline)
		if e != nil {
			if errors.Is(e, context.DeadlineExceeded) && ctx.Err() == nil {
				return positive, nil
			}
			return nil, e
		}
		for _, f := range frames {
			d := f.Data
			// Functional response is a complete single ISO-TP frame, not an arbitrary
			// frame whose second byte happens to resemble a positive service.
			if f.Bus == bus && responders[f.Address] && len(d) >= 2 && d[0] > 0 && d[0] <= 7 && int(d[0]) < len(d) && int(d[1]) == int(mode)+0x40 {
				positive[f.Address] = true
			}
		}
		if len(positive) == len(responders) {
			return positive, nil
		}
		if e := rawWait(deadline, time.Millisecond); e != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return positive, nil
		}
	}
}
