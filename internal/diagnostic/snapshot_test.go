package diagnostic

import (
	"context"
	"fmt"
	"github.com/kaikozlov/toyota-diagnostics/internal/transport"
	"path/filepath"
	"reflect"
	"testing"
)

func snapshotFixture(status int, raw string) map[string]any {
	return map[string]any{"schema": SnapshotSchema, "vehicle_type": 123, "ecus": []map[string]any{{"category_id": 3, "address": 0x700, "sub_addr": nil, "key": "ecu", "name": "ECU", "mount": map[string]any{"live_state": "responding"}, "dtc": map[string]any{"state": "positive", "records": []map[string]any{{"code": "P123456", "status": status}}}, "freeze_frames": map[string]any{"dtcs": []map[string]any{{"dtc": "P123456", "state": "positive", "records": []map[string]any{{"record_number": 1, "identifiers": []map[string]any{{"did": 0x1000, "data_hex": raw}}}}}}}}}}
}
func TestSnapshotErrorClassification(t *testing.T) {
	if got := snapshotErrorState(&transport.NegativeResponseError{Service: 0x19, Code: 0x31}); got != "negative_response" {
		t.Fatal(got)
	}
	if got := snapshotErrorState(context.DeadlineExceeded); got != "no_response" {
		t.Fatal(got)
	}
}
func TestSnapshotSaveLoadAndDiff(t *testing.T) {
	a, b := snapshotFixture(1, "aa"), snapshotFixture(2, "bb")
	resolveRows(resolveMap(resolveRows(b["ecus"])[0]["dtc"])["records"])[0]["status"] = byte(2)
	path := filepath.Join(t.TempDir(), "nested", "snapshot.json")
	if e := SaveSnapshot(a, path); e != nil {
		t.Fatal(e)
	}
	loaded, e := LoadSnapshot(path)
	if e != nil {
		t.Fatal(e)
	}
	d, e := CompareSnapshots(loaded, b)
	if e != nil {
		t.Fatal(e)
	}
	rows := resolveRows(d["changes"])
	if len(rows) != 1 {
		t.Fatal(d)
	}
	if resolveMap(d["summary"])["dtc_changes"] != 1 || resolveMap(d["summary"])["freeze_frame_changes"] != 1 {
		t.Fatal(d)
	}
	same, e := CompareSnapshots(a, a)
	if e != nil || len(resolveRows(same["changes"])) != 0 {
		t.Fatal(same, e)
	}
}

type snapshotTestClient struct {
	responses map[string][]byte
	calls     []string
}

func (c *snapshotTestClient) Request(ctx context.Context, sid byte, data []byte) ([]byte, error) {
	key := fmt.Sprintf("%02x%x", sid, data)
	c.calls = append(c.calls, key)
	b, ok := c.responses[key]
	if !ok {
		return nil, fmt.Errorf("unexpected %s", key)
	}
	return b, nil
}
func TestSnapshotFreezeFramesSubfunction(t *testing.T) {
	c := &snapshotTestClient{responses: map[string][]byte{"1904123456ff": recorderHex("04123456010101123402aabb")}}
	d := SnapshotFreezeFrames(context.Background(), c, map[string]any{}, []map[string]any{{"code": "P123456"}}, nil)
	if d["positive_dtc_count"] != 1 || d["record_count"] != 1 {
		t.Fatal(d)
	}
	rows := resolveRows(d["dtcs"])
	if rows[0]["status"] != 1 {
		t.Fatal(rows)
	}
}
func TestSnapshotRoBParsesInventoryFrameRecord(t *testing.T) {
	c := &snapshotTestClient{responses: map[string][]byte{"ab01": recorderHex("011234"), "ab021234": recorderHex("02dead0101"), "ab0312340101": recorderHex("03deadbeef01100101aa")}}
	command := map[string]any{"protocols": []map[string]any{{"inventory": map[string]any{"send": "ab01", "check": "eb01"}, "frames": map[string]any{"send": "ab020000", "check": "eb02"}, "record": map[string]any{"send": "ab0300000000", "check": "eb03"}}}}
	d := SnapshotRoB(context.Background(), c, command, nil)
	if d["behavior_code_count"] != 1 || d["frame_count"] != 1 || d["record_count"] != 1 || d["did_block_count"] != 1 {
		t.Fatal(d)
	}
	if !reflect.DeepEqual(c.calls, []string{"ab01", "ab021234", "ab0312340101"}) {
		t.Fatal(c.calls)
	}
}
func TestSnapshotPlanRequiresExactContract(t *testing.T) {
	catalog := map[string]any{"commands": []map[string]any{{"kind": "p5_dtc_snapshot", "execution": "read_only", "plugin_binding": map[string]any{"dll": "GetEachFrzFrmDatP5_DT.dll"}, "requests": []map[string]any{{"resolved": true, "send": "1904000000ff", "check": "5904"}}}}}
	if P5SnapshotPlan(catalog) == nil {
		t.Fatal("missing exact plan")
	}
	resolveRows(resolveRows(catalog["commands"])[0]["requests"])[0]["send"] = "1905000000ff"
	if P5SnapshotPlan(catalog) != nil {
		t.Fatal("guessed unsupported plan")
	}
}
