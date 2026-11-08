package diagnostic

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strconv"
)

// RecorderBlock retains wire bytes; consumers decide which catalog decoder applies.
type RecorderBlock struct {
	DataID int    `json:"data_id"`
	Length int    `json:"length"`
	Data   []byte `json:"-"`
}
type RecorderRecord struct {
	Behavior           int             `json:"behavior,omitempty"`
	Record             int             `json:"record,omitempty"`
	BehaviorEcho       int             `json:"behavior_echo,omitempty"`
	FrameEcho          int             `json:"frame_echo,omitempty"`
	ROB                int             `json:"rob,omitempty"`
	Frame              uint32          `json:"frame,omitempty"`
	DeclaredBlockCount *int            `json:"declared_block_count,omitempty"`
	BlockCount         int             `json:"block_count"`
	Blocks             []RecorderBlock `json:"blocks"`
}

func Level49Key(seed []byte) ([]byte, error) {
	if len(seed) != 6 {
		return nil, fmt.Errorf("TSS3 Image FFD level-49 seed must be six bytes")
	}
	out := make([]byte, 6)
	rotations := []uint{1, 2, 3, 3, 2, 1}
	for i, v := range seed {
		idx := int(v & 7)
		if idx >= 6 {
			idx -= 6
		}
		add := seed[idx]
		if idx < i {
			add = out[idx]
		}
		n := uint((v>>rotations[i])&3) + 1
		out[i] = (v<<n | v>>(8-n)) + add
	}
	return out, nil
}
func recorderPrefix(response, prefix []byte, what string) error {
	if !bytes.HasPrefix(response, prefix) {
		return fmt.Errorf("%s: expected %X response, got %X", what, prefix, response)
	}
	return nil
}
func recorderWords(data []byte, what string) ([]int, error) {
	if len(data)%2 != 0 {
		return nil, fmt.Errorf("%s: odd trailing payload length %d", what, len(data))
	}
	out := make([]int, 0, len(data)/2)
	for i := 0; i < len(data); i += 2 {
		out = append(out, int(binary.BigEndian.Uint16(data[i:])))
	}
	return out, nil
}
func recorderUnique(values []int) []int {
	seen := map[int]bool{}
	out := make([]int, 0, len(values))
	for _, v := range values {
		if !seen[v] {
			seen[v] = true
			out = append(out, v)
		}
	}
	sort.Ints(out)
	return out
}
func ParseOperationBehaviors(response []byte) ([]int, error) {
	if err := recorderPrefix(response, []byte{0xeb, 0x11}, "Operation FFD behavior enumeration"); err != nil {
		return nil, err
	}
	return recorderWords(response[2:], "Operation FFD behavior enumeration")
}
func ParseOperationRecords(response []byte, behavior int) ([]int, error) {
	if err := recorderPrefix(response, []byte{0xeb, 0x12}, "Operation FFD record enumeration"); err != nil {
		return nil, err
	}
	if len(response) < 4 || int(binary.BigEndian.Uint16(response[2:])) != behavior {
		return nil, fmt.Errorf("Operation FFD record enumeration: behavior echo mismatch for 0x%04X", behavior)
	}
	rows, err := recorderWords(response[4:], "Operation FFD record enumeration")
	return recorderUnique(rows), err
}
func parseRecorderBlocks(payload []byte, count *int, extended bool, what string) ([]RecorderBlock, error) {
	blocks := []RecorderBlock{}
	offset := 0
	for offset < len(payload) && (count == nil || len(blocks) < *count) {
		if len(payload)-offset < 3 {
			return nil, fmt.Errorf("%s: truncated block header at byte %d", what, offset)
		}
		id := int(binary.BigEndian.Uint16(payload[offset:]))
		offset += 2
		length := uint64(payload[offset])
		offset++
		if extended && id >= 0x6000 && id <= 0x6fff {
			offset--
			if len(payload)-offset < 4 {
				return nil, fmt.Errorf("%s: truncated BE32 length for DID 0x%04X", what, id)
			}
			length = uint64(binary.BigEndian.Uint32(payload[offset:]))
			offset += 4
		}
		if length > uint64(len(payload)-offset) {
			return nil, fmt.Errorf("%s: DID 0x%04X length %d exceeds response", what, id, length)
		}
		data := append([]byte(nil), payload[offset:offset+int(length)]...)
		offset += int(length)
		blocks = append(blocks, RecorderBlock{id, len(data), data})
	}
	if count != nil && len(blocks) != *count {
		return nil, fmt.Errorf("%s: expected %d blocks, parsed %d", what, *count, len(blocks))
	}
	if offset != len(payload) {
		return nil, fmt.Errorf("%s: %d trailing byte(s)", what, len(payload)-offset)
	}
	return blocks, nil
}
func dedupRecorderBlocks(blocks []RecorderBlock) []RecorderBlock {
	seen := map[int]bool{}
	out := make([]RecorderBlock, 0, len(blocks))
	for _, b := range blocks {
		if !seen[b.DataID] {
			seen[b.DataID] = true
			out = append(out, b)
		}
	}
	return out
}
func ParseP5RoBBehaviorCodes(response, prefix []byte) ([]int, error) {
	if err := recorderPrefix(response, prefix, "P5 RoB behavior enumeration"); err != nil {
		return nil, err
	}
	if len(prefix) != 2 {
		return nil, fmt.Errorf("P5 RoB behavior enumeration: positive prefix must be two bytes")
	}
	return recorderWords(response[2:], "P5 RoB behavior enumeration")
}
func ParseP5RoBFrames(response, prefix []byte) (map[string]any, error) {
	if err := recorderPrefix(response, prefix, "P5 RoB frame enumeration"); err != nil {
		return nil, err
	}
	if len(prefix) != 2 || len(response) < 4 {
		return nil, fmt.Errorf("P5 RoB frame enumeration: truncated response header")
	}
	frames, err := recorderWords(response[4:], "P5 RoB frame enumeration")
	if err != nil {
		return nil, err
	}
	return map[string]any{"behavior_echo": int(binary.BigEndian.Uint16(response[2:])), "frames": recorderUnique(frames)}, nil
}
func ParseP5RoBRecord(response, prefix []byte) (RecorderRecord, error) {
	var result RecorderRecord
	if err := recorderPrefix(response, prefix, "P5 RoB record"); err != nil {
		return result, err
	}
	if len(prefix) != 2 || len(response) < 6 {
		return result, fmt.Errorf("P5 RoB record: truncated response header")
	}
	result.BehaviorEcho = int(binary.BigEndian.Uint16(response[2:]))
	result.FrameEcho = int(binary.BigEndian.Uint16(response[4:]))
	result.Blocks = []RecorderBlock{}
	if len(response) == 6 {
		return result, nil
	}
	declared := int(response[6])
	result.DeclaredBlockCount = &declared
	count := &declared
	if declared == 0 {
		count = nil
	}
	blocks, err := parseRecorderBlocks(response[7:], count, true, "P5 RoB record")
	if err != nil {
		return result, err
	}
	result.BlockCount = len(blocks)
	result.Blocks = dedupRecorderBlocks(blocks)
	return result, nil
}
func ParseOperationRecord(response []byte, behavior, record int) (RecorderRecord, error) {
	var result RecorderRecord
	if err := recorderPrefix(response, []byte{0xeb, 0x13}, "Operation FFD record"); err != nil {
		return result, err
	}
	if len(response) < 6 {
		return result, fmt.Errorf("Operation FFD record: truncated response header")
	}
	gotBehavior := int(binary.BigEndian.Uint16(response[2:]))
	gotRecord := int(binary.BigEndian.Uint16(response[4:]))
	if gotBehavior != behavior || gotRecord != record {
		return result, fmt.Errorf("Operation FFD record: echo mismatch, expected 0x%04X/0x%04X, got 0x%04X/0x%04X", behavior, record, gotBehavior, gotRecord)
	}
	result.Behavior = behavior
	result.Record = record
	result.Blocks = []RecorderBlock{}
	payload := response[6:]
	if len(payload) == 0 {
		return result, nil
	}
	count := int(payload[0])
	blocks, err := parseRecorderBlocks(payload[1:], &count, false, "Operation FFD record")
	if err == nil {
		result.DeclaredBlockCount = &count
	} else {
		blocks, err = parseRecorderBlocks(payload, nil, false, "Operation FFD record")
	}
	if err != nil {
		return result, err
	}
	result.BlockCount = len(blocks)
	result.Blocks = dedupRecorderBlocks(blocks)
	return result, nil
}
func ParseImageRoBs(response []byte) ([]int, error) {
	if err := recorderPrefix(response, []byte{0xeb, 0x31}, "Image FFD RoB enumeration"); err != nil {
		return nil, err
	}
	return recorderWords(response[2:], "Image FFD RoB enumeration")
}
func ParseImageRecord(response []byte, rob int, frame uint32) (RecorderRecord, error) {
	var result RecorderRecord
	if err := recorderPrefix(response, []byte{0xeb, 0x33}, "Image FFD record"); err != nil {
		return result, err
	}
	if len(response) < 9 {
		return result, fmt.Errorf("Image FFD record: truncated response header")
	}
	gotROB := int(binary.BigEndian.Uint16(response[2:]))
	gotFrame := binary.BigEndian.Uint32(response[4:])
	if gotROB != rob || gotFrame != frame {
		return result, fmt.Errorf("Image FFD record: echo mismatch, expected 0x%04X/0x%08X, got 0x%04X/0x%08X", rob, frame, gotROB, gotFrame)
	}
	declared := int(response[8])
	count := &declared
	if declared == 0 {
		count = nil
	}
	blocks, err := parseRecorderBlocks(response[9:], count, true, "Image FFD record")
	if err != nil {
		return result, err
	}
	result.ROB = rob
	result.Frame = frame
	result.DeclaredBlockCount = &declared
	result.BlockCount = len(blocks)
	result.Blocks = blocks
	return result, nil
}
func ImageFrameOccurrence(split, dataSet, trigger int) uint32 {
	return uint32(split*0x200 + dataSet*10 + trigger - 10)
}
func recorderNumber(v any) (float64, error) {
	switch n := v.(type) {
	case float64:
		return n, nil
	case int:
		return float64(n), nil
	case int64:
		return float64(n), nil
	case string:
		return strconv.ParseFloat(n, 64)
	default:
		return 0, fmt.Errorf("expected number, got %v", v)
	}
}
func DecodeRecorderSignal(data []byte, row map[string]any) (map[string]any, error) {
	integer := func(key string) (int, error) { n, e := recorderNumber(row[key]); return int(n), e }
	width, e := integer("BitLength")
	if e != nil {
		return nil, e
	}
	pos, e := integer("BytePosition")
	if e != nil {
		return nil, e
	}
	bit, e := integer("BitPosition")
	if e != nil {
		return nil, e
	}
	start := (pos-1)*8 + 7 - bit
	if width <= 0 || width > 64 || pos <= 0 || bit < 0 || bit > 7 || start+width > len(data)*8 {
		return nil, fmt.Errorf("recorder field %v exceeds %d-byte payload or supported 64-bit width", row["DataName"], len(data))
	}
	var bits uint64
	for i := start; i < start+width; i++ {
		bits = bits<<1 | uint64((data[i/8]>>uint(7-i%8))&1)
	}
	typ, _ := row["Type"].(string)
	var raw any
	var numeric float64
	switch typ {
	case "f":
		if width != 32 {
			return nil, fmt.Errorf("unsupported float width %d", width)
		}
		numeric = float64(math.Float32frombits(uint32(bits)))
		raw = numeric
	case "d":
		if width != 64 {
			return nil, fmt.Errorf("unsupported double width %d", width)
		}
		numeric = math.Float64frombits(bits)
		raw = numeric
	case "s":
		signed := int64(bits)
		if width < 64 && bits&(uint64(1)<<uint(width-1)) != 0 {
			signed -= int64(uint64(1) << uint(width))
		}
		raw = signed
		numeric = float64(signed)
	default:
		raw = bits
		numeric = float64(bits)
	}
	scale, e := recorderNumber(row["Lsb"])
	if e != nil {
		return nil, e
	}
	offset, e := recorderNumber(row["Offset"])
	if e != nil {
		return nil, e
	}
	point, e := integer("Point")
	if e != nil {
		return nil, e
	}
	if point < 0 || point > 100 {
		return nil, fmt.Errorf("invalid recorder decimal precision %d", point)
	}
	value := numeric*scale + offset
	return map[string]any{"name": row["DataName"], "raw": raw, "value": value, "formatted": fmt.Sprintf("%.*f", point, value), "type": typ, "byte_position": pos, "bit_position": bit, "bit_length": width}, nil
}
func DecodeRecorderBlock(block RecorderBlock, signals []map[string]any) map[string]any {
	decoded := []map[string]any{}
	errors := []string{}
	for _, row := range signals {
		value, err := DecodeRecorderSignal(block.Data, row)
		if err != nil {
			errors = append(errors, err.Error())
		} else {
			decoded = append(decoded, value)
		}
	}
	return map[string]any{"data_id": block.DataID, "length": len(block.Data), "data_hex": hex.EncodeToString(block.Data), "signals": decoded, "decode_errors": errors}
}
