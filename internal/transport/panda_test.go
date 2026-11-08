package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"
	"time"
)

func TestPandaAdapterOwnershipAndRawProtocol(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("Python unavailable; Panda adapter tests require Python")
	}
	// Stub upstream modules, not Toyota logic: adapter must never import Toyota.
	prelude := `
import sys, types
class FakePanda:
    def __init__(self): print("upstream diagnostic")
    def set_safety_mode(self, model, param):
        assert model == "elm327" and param == 1
    def can_send(self, address, data, bus):
        assert (address, data, bus) == (2016, bytes.fromhex("0322f19000000000"), 0)
    def can_recv(self): return [(2024, bytes.fromhex("0362f190"), 0)]
    def can_clear(self, flags): assert flags == 65535
    def close(self): pass
sys.modules["panda"] = types.SimpleNamespace(Panda=FakePanda)
sys.modules["opendbc.car.structs"] = types.SimpleNamespace(CarParams=types.SimpleNamespace(SafetyModel=types.SimpleNamespace(elm327="elm327")))
import subprocess
subprocess.run = lambda *a, **k: types.SimpleNamespace(returncode=1)
`
	cmd := exec.Command(python, "-c", prelude+"\n"+pandaAdapter)
	cmd.Stdin = bytes.NewBufferString("{\"op\":\"open\"}\n{\"op\":\"send\",\"address\":2016,\"data\":\"0322f19000000000\",\"bus\":0}\n{\"op\":\"recv\"}\n{\"op\":\"clear\"}\n{\"op\":\"close\"}\n")
	out, e := cmd.Output()
	if e != nil {
		t.Fatal(e)
	}
	lines := bytes.Split(bytes.TrimSpace(out), []byte("\n"))
	if len(lines) != 5 {
		t.Fatalf("protocol polluted: %s", out)
	}
	for _, line := range lines {
		var r pandaReply
		if e := json.Unmarshal(line, &r); e != nil || !r.OK {
			t.Fatalf("%s: %v", line, e)
		}
	}
	var r pandaReply
	_ = json.Unmarshal(lines[2], &r)
	if len(r.Frames) != 1 || r.Frames[0].Data != "0362f190" {
		t.Fatalf("%s", lines[2])
	}
}
func TestManagedRechecksSafetyAndNeverChangesIt(t *testing.T) {
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Skip("Python unavailable")
	}
	script := "namespace={'__name__':'adapter'}\nexec(" + string(mustJSON(pandaAdapter)) + ",namespace)\n" + `
import types
states = [types.SimpleNamespace(safetyModel="elm327")]
class SM:
    def __getitem__(self, key): return states
    def update(self, timeout): pass
class Pub:
    def send(self, data): sent.append(data)
sent=[]
messaging=types.SimpleNamespace(sub_sock=lambda *a,**k:object(),pub_sock=lambda *a:Pub(),SubMaster=lambda *a:SM(),drain_sock=lambda *a,**k:[])
import sys
openpilot=types.ModuleType("openpilot");cereal=types.ModuleType("openpilot.cereal")
cereal.messaging=messaging;openpilot.cereal=cereal
sys.modules["openpilot"]=openpilot;sys.modules["openpilot.cereal"]=cereal
sys.modules["openpilot.cereal.messaging"]=messaging
sys.modules["openpilot.selfdrive.pandad"]=types.SimpleNamespace(can_list_to_can_capnp=lambda frames,**k:frames)
m=namespace["Managed"]()
m.can_send(2016,b"x",0)
assert len(sent)==1
states[0].safetyModel="toyota"
try:
    m.can_send(2016,b"x",0)
except RuntimeError:
    pass
else:
    raise AssertionError("sent after leaving diagnostic safety")
assert len(sent)==1
`
	if out, e := exec.Command(python, "-c", script).CombinedOutput(); e != nil {
		t.Fatalf("%v: %s", e, out)
	}
}
func mustJSON(v string) []byte {
	b, e := json.Marshal(v)
	if e != nil {
		panic(e)
	}
	return b
}

func TestPandaCancellationPreservesWorkerForCleanup(t *testing.T) {
	python, e := exec.LookPath("python3")
	if e != nil {
		t.Skip("Python unavailable")
	}
	cmd := exec.Command(python, "-u", "-c", `import sys,json,time
for line in sys.stdin:
    request=json.loads(line)
    if request["op"]=="slow": time.sleep(0.1)
    print(json.dumps({"ok":True,"op":request["op"]}),flush=True)
`)
	in, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal(e)
	}
	out, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal(e)
	}
	if e = cmd.Start(); e != nil {
		t.Fatal(e)
	}
	p := &Panda{cmd: cmd, in: in, out: bufio.NewReader(out)}
	defer p.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if e = p.exchange(ctx, map[string]string{"op": "slow"}, nil); !errors.Is(e, context.DeadlineExceeded) {
		t.Fatalf("%v", e)
	}
	cleanup, c := context.WithTimeout(context.Background(), time.Second)
	defer c()
	var result map[string]any
	if e = p.exchange(cleanup, map[string]string{"op": "stop"}, &result); e != nil {
		t.Fatal(e)
	}
	if result["op"] != "stop" {
		t.Fatalf("stale operation response consumed by cleanup: %v", result)
	}
}
