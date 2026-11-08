package diagnostic

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
)

const FunctionalOBDRequestAddress = 0x7DF
const ClearUDSTimeout = 2 * time.Second

func DecodeStatusBits(status int) ([]string, error) {
	if status < 0 || status > 255 {
		return nil, fmt.Errorf("status byte out of range: %#x", status)
	}
	names := []string{"TEST_FAILED", "TEST_FAILED_THIS_OPERATION_CYCLE", "PENDING_DTC", "CONFIRMED_DTC", "TEST_NOT_COMPLETED_SINCE_LAST_CLEAR", "TEST_FAILED_SINCE_LAST_CLEAR", "TEST_NOT_COMPLETED_THIS_OPERATION_CYCLE", "WARNING_INDICATOR_REQUESTED"}
	out := []string{}
	for i, name := range names {
		if status&(1<<i) != 0 {
			out = append(out, name)
		}
	}
	return out, nil
}

type DTCRecord struct {
	Code   string `json:"code"`
	Status byte   `json:"status"`
}

func DTCNumberToString(n uint32) string { return fmt.Sprintf("%c%06X", "PCBU"[(n>>22)&3], n&0x3fffff) }
func DTCStringToNumber(code string) (uint32, error) {
	s := strings.ToUpper(strings.TrimSpace(code))
	if len(s) != 7 || !strings.ContainsRune("PCBU", rune(s[0])) {
		return 0, fmt.Errorf("invalid 3-byte DTC code %q", code)
	}
	n, e := strconv.ParseUint(s[1:], 16, 24)
	if e != nil {
		return 0, fmt.Errorf("invalid 3-byte DTC code %q", code)
	}
	if n&0xc00000 != 0 {
		return 0, fmt.Errorf("invalid ISO 15031-6 DTC code %q", code)
	}
	return uint32(n) | uint32(strings.IndexByte("PCBU", s[0]))<<22, nil
}
func ParseDTCResponse(data []byte) ([]DTCRecord, error) {
	out := []DTCRecord{}
	if len(data) == 0 {
		return out, nil
	}
	if (len(data)-1)%4 != 0 {
		return nil, fmt.Errorf("malformed DTC response length %d", len(data))
	}
	for i := 1; i < len(data); i += 4 {
		n := uint32(data[i])<<16 | uint32(data[i+1])<<8 | uint32(data[i+2])
		out = append(out, DTCRecord{DTCNumberToString(n), data[i+3]})
	}
	return out, nil
}
func FFDType(record byte) int {
	switch {
	case record == 1 || record == 2:
		return int(record)
	case record >= 0x10 && record < 0x30:
		return 0x10
	case record >= 0x30 && record < 0x50:
		return 0x30
	case record >= 0x50 && record < 0x70:
		return 0x50
	}
	return 0
}

// ParseP5DTCSnapshotResponse parses data following the positive SID/subfunction 59 04.
func ParseP5DTCSnapshotResponse(data []byte, expectedDTC uint32) (map[string]any, error) {
	if expectedDTC > 0xffffff {
		return nil, fmt.Errorf("DTC does not fit 24 bits: %#x", expectedDTC)
	}
	if len(data) < 4 {
		return nil, fmt.Errorf("malformed P5 DTC snapshot response length %d", len(data))
	}
	got := uint32(data[0])<<16 | uint32(data[1])<<8 | uint32(data[2])
	if got != expectedDTC {
		return nil, fmt.Errorf("P5 DTC snapshot echo mismatch: expected %06X, got %06X", expectedDTC, got)
	}
	offset := 4
	records := []map[string]any{}
	for offset < len(data) {
		if len(data)-offset < 2 {
			return nil, fmt.Errorf("truncated P5 DTC snapshot record header at offset %d", offset)
		}
		record, count := data[offset], data[offset+1]
		offset += 2
		kind := FFDType(record)
		if kind == 0 {
			break
		}
		identifiers := []map[string]any{}
		for i := 0; i < int(count); i++ {
			if len(data)-offset < 3 {
				return nil, fmt.Errorf("truncated P5 DTC snapshot DID header at offset %d", offset)
			}
			did := int(data[offset])<<8 | int(data[offset+1])
			length := int(data[offset+2])
			offset += 3
			if len(data)-offset < length {
				return nil, fmt.Errorf("P5 DTC snapshot DID 0x%04X length %d exceeds response", did, length)
			}
			identifiers = append(identifiers, map[string]any{"did": did, "length": length, "data_hex": hex.EncodeToString(data[offset : offset+length])})
			offset += length
		}
		records = append(records, map[string]any{"record_number": int(record), "ffd_type": kind, "identifier_count": int(count), "identifiers": identifiers})
	}
	for _, b := range data[offset:] {
		if b != 0 {
			return nil, fmt.Errorf("P5 DTC snapshot response has %d unparsed trailing byte(s)", len(data)-offset)
		}
	}
	return map[string]any{"dtc": DTCNumberToString(expectedDTC), "dtc_raw": expectedDTC, "status": int(data[3]), "records": records}, nil
}
