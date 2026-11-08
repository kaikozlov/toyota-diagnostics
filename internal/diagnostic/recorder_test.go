package diagnostic

import (
	"encoding/hex"
	"reflect"
	"testing"
)

func recorderHex(s string) []byte { b, _ := hex.DecodeString(s); return b }
func TestLevel49Vectors(t *testing.T) {
	for seed, key := range map[string]string{"000000000000": "000000000000", "010203040506": "04070a0d1a64", "123456789abc": "9e6a50252409", "deadbeefcafe": "cbd8b6970cba", "690f82163710": "e1ff8791db01"} {
		got, e := Level49Key(recorderHex(seed))
		if e != nil || hex.EncodeToString(got) != key {
			t.Fatalf("%s -> %x: %v", seed, got, e)
		}
	}
}
func TestRecorderWireForms(t *testing.T) {
	ids, e := ParseOperationBehaviors(recorderHex("eb1128182845"))
	if e != nil || !reflect.DeepEqual(ids, []int{0x2818, 0x2845}) {
		t.Fatal(ids, e)
	}
	ids, e = ParseOperationRecords(recorderHex("eb122818010101000101"), 0x2818)
	if e != nil || !reflect.DeepEqual(ids, []int{0x100, 0x101}) {
		t.Fatal(ids, e)
	}
	r, e := ParseOperationRecord(recorderHex("eb132818010002523d04c040000056310500ffc76400"), 0x2818, 0x100)
	if e != nil || r.BlockCount != 2 || r.Blocks[0].DataID != 0x523d {
		t.Fatal(r, e)
	}
	r, e = ParseP5RoBRecord(recorderHex("eb13deadbeef02123402aabb600200000003112233"), recorderHex("eb13"))
	if e != nil || r.BehaviorEcho != 0xdead || r.FrameEcho != 0xbeef || r.BlockCount != 2 || r.Blocks[1].Length != 3 {
		t.Fatal(r, e)
	}
	r, e = ParseP5RoBRecord(recorderHex("eb031234010100123401aa123401bb"), recorderHex("eb03"))
	if e != nil || r.BlockCount != 2 || len(r.Blocks) != 1 || hex.EncodeToString(r.Blocks[0].Data) != "aa" {
		t.Fatal(r, e)
	}
	r, e = ParseImageRecord(recorderHex("eb3328220000020102050102aabb600200000003112233"), 0x2822, 0x201)
	if e != nil || r.BlockCount != 2 || ImageFrameOccurrence(1, 1, 1) != 0x201 {
		t.Fatal(r, e)
	}
}
func TestRecorderRejectsMalformed(t *testing.T) {
	for _, s := range []string{"eb33", "eb33282200000201016002ffffffff", "eb3328220000020102050102aa"} {
		if _, e := ParseImageRecord(recorderHex(s), 0x2822, 0x201); e == nil {
			t.Fatal("accepted", s)
		}
	}
	if _, e := ParseOperationRecord(recorderHex("eb1328190100"), 0x2818, 0x100); e == nil {
		t.Fatal("accepted wrong echo")
	}
}
func TestRecorderIEEEAndSigned(t *testing.T) {
	row := map[string]any{"Type": "f", "BitLength": 32, "BytePosition": 1, "BitPosition": 7, "Lsb": "1", "Offset": "0", "Point": 3, "DataName": "angle"}
	v, e := DecodeRecorderSignal(recorderHex("c0400000"), row)
	if e != nil || v["formatted"] != "-3.000" {
		t.Fatal(v, e)
	}
	row["Type"] = "s"
	row["BitLength"] = 16
	row["Lsb"] = "0.001"
	row["Point"] = 3
	v, e = DecodeRecorderSignal(recorderHex("ffc7"), row)
	if e != nil || v["formatted"] != "-0.057" {
		t.Fatal(v, e)
	}
}
