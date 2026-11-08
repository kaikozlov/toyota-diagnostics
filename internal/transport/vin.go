package transport

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type VINResponse struct {
	VIN     string
	Address uint32
	Bus     int
}
type vinAssembly struct {
	length   int
	data     []byte
	sequence byte
}

var standardVINResponses = []uint32{0x7e8, 0x7ea, 0x768, 0x7ce, 0x18daf110, 0x18daf10e}

// FunctionalVIN sends standard UDS and legislated OBD VIN queries in both CAN
// addressing formats. Every responding ISO-TP first frame receives physical
// flow control, including nonpreferred ECUs, without mixing their sequences.
func FunctionalVIN(ctx context.Context, can CAN, bus int, timeout time.Duration) (VINResponse, error) {
	if timeout <= 0 {
		timeout = 100 * time.Millisecond
	}
	for _, q := range [][]byte{{0x22, 0xf1, 0x90}, {9, 2}} {
		result, err := functionalVINQuery(ctx, can, bus, q, timeout)
		if err == nil {
			return result, nil
		}
		if !errors.Is(err, context.DeadlineExceeded) {
			return VINResponse{}, err
		}
		if ctx.Err() != nil {
			return VINResponse{}, ctx.Err()
		}
	}
	return VINResponse{}, context.DeadlineExceeded
}
func functionalVINPhysical(rx uint32) (uint32, bool) {
	if rx >= 0x708 && rx <= 0x7ff && rx != 0x7e7 {
		return rx - 8, true
	}
	if rx&0x1fffff00 == 0x18daf100 {
		return 0x18da00f1 | ((rx & 255) << 8), true
	}
	return 0, false
}
func vinString(data []byte) (string, bool) {
	// Upstream allows null-padded Ford/Nissan VINs and Honda's length byte.
	data = bytes.TrimRight(data, "\x00")
	if len(data) > 0 && data[0] == 0x11 {
		data = data[1:]
	}
	if len(data) != 17 {
		return "", false
	}
	for _, c := range data {
		if !strings.ContainsRune("ABCDEFGHJKLMNPRSTUVWXYZ0123456789", rune(c)) {
			return "", false
		}
	}
	if bytes.Equal(data, bytes.Repeat([]byte{'0'}, 17)) {
		return "", false
	}
	return string(data), true
}
func functionalVINQuery(ctx context.Context, can CAN, bus int, request []byte, timeout time.Duration) (VINResponse, error) {
	if err := ctx.Err(); err != nil {
		return VINResponse{}, err
	}
	if err := can.Clear(ctx); err != nil {
		return VINResponse{}, err
	}
	frame := make([]byte, 8)
	frame[0] = byte(len(request))
	copy(frame[1:], request)
	for _, address := range []uint32{0x7df, 0x18db33f1} {
		if err := can.Send(ctx, Frame{Address: address, Data: frame, Bus: bus}); err != nil {
			return VINResponse{}, err
		}
	}
	prefix := []byte{0x62, 0xf1, 0x90}
	if request[0] == 9 {
		prefix = []byte{0x49, 2, 1}
	}
	assemblies := map[uint32]*vinAssembly{}
	results := map[uint32]string{}
	record := func(address uint32, data []byte) {
		if bytes.HasPrefix(data, prefix) {
			if vin, ok := vinString(data[len(prefix):]); ok {
				results[address] = vin
			}
		}
	}
	deadline, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	for {
		frames, err := can.Receive(deadline)
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				break
			}
			return VINResponse{}, err
		}
		for _, f := range frames {
			tx, ok := functionalVINPhysical(f.Address)
			if !ok || f.Bus != bus || len(f.Data) < 2 {
				continue
			}
			d := f.Data
			switch d[0] >> 4 {
			case 0:
				n := int(d[0] & 15)
				if n > 0 && n < len(d) {
					record(f.Address, d[1:1+n])
				}
			case 1:
				n := int(d[0]&15)<<8 | int(d[1])
				if n <= 7 || n > 4095 {
					continue
				}
				assemblies[f.Address] = &vinAssembly{length: n, data: append([]byte(nil), d[2:]...), sequence: 1}
				if err := can.Send(deadline, Frame{Address: tx, Data: []byte{0x30, 0, 0, 0, 0, 0, 0, 0}, Bus: bus}); err != nil {
					return VINResponse{}, err
				}
			case 2:
				a := assemblies[f.Address]
				if a == nil {
					continue
				}
				if d[0]&15 != a.sequence {
					delete(assemblies, f.Address)
					continue
				}
				a.sequence = (a.sequence + 1) & 15
				a.data = append(a.data, d[1:]...)
				if len(a.data) >= a.length {
					record(f.Address, a.data[:a.length])
					delete(assemblies, f.Address)
				}
			}
		}
		if err := sleep(deadline, time.Millisecond); err != nil {
			break
		}
	}
	if err := ctx.Err(); err != nil {
		return VINResponse{}, err
	}
	for _, address := range standardVINResponses {
		if vin, ok := results[address]; ok {
			return VINResponse{vin, address, bus}, nil
		}
	}
	// Unknown responders are still useful, but never silently choose among
	// conflicting VINs from nonstandard ECUs.
	var chosen VINResponse
	for address, vin := range results {
		if chosen.VIN != "" && chosen.VIN != vin {
			return VINResponse{}, fmt.Errorf("functional VIN responders disagree")
		}
		if chosen.VIN == "" || address < chosen.Address {
			chosen = VINResponse{vin, address, bus}
		}
	}
	if chosen.VIN != "" {
		return chosen, nil
	}
	return VINResponse{}, context.DeadlineExceeded
}
