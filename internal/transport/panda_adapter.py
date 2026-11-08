"""Private raw CAN I/O adapter. No Toyota metadata, UDS, or diagnostic operations."""
import contextlib
import json
import signal
import subprocess
import sys
import time


def pandad_running():
    for command in (["pidof", "pandad"], ["pgrep", "-x", "pandad"]):
        try:
            result = subprocess.run(command, stdout=subprocess.DEVNULL, check=False)
        except FileNotFoundError:
            continue
        if result.returncode in (0, 1):
            return result.returncode == 0
        raise RuntimeError("cannot verify Panda ownership")
    raise RuntimeError("cannot verify Panda ownership: neither pidof nor pgrep is available")


def managed_states():
    import openpilot.cereal.messaging as messaging
    sm = messaging.SubMaster(["pandaStates"])
    deadline = time.monotonic() + 1
    while not len(sm["pandaStates"]) and time.monotonic() < deadline:
        sm.update(100)
    assert_ready(sm)
    return sm


def assert_ready(sm):
    sm.update(0)
    states = sm["pandaStates"]
    if len(states) != 1:
        raise RuntimeError(f"expected one Panda for managed diagnostics, got {len(states)}")
    if str(states[0].safetyModel) != "elm327":
        raise RuntimeError("pandad is running but Panda is not in ELM327 diagnostic safety; stop openpilot/manager for direct diagnostics")


class Managed:
    def __init__(self, passive=False):
        import openpilot.cereal.messaging as messaging
        self.messaging = messaging
        self.sock = messaging.sub_sock("can", conflate=False, timeout=100)
        self.passive = passive
        if not passive:
            from openpilot.selfdrive.pandad import can_list_to_can_capnp
            self.serialize = can_list_to_can_capnp
            self.pub = messaging.pub_sock("sendcan")
            self.sm = managed_states()
            time.sleep(0.15)

    def ready(self):
        assert_ready(self.sm)

    def can_recv(self):
        return [(m.address, bytes(m.dat), m.src)
                for event in self.messaging.drain_sock(self.sock, wait_for_one=False)
                for m in event.can]

    def can_send(self, address, data, bus):
        if self.passive:
            raise RuntimeError("receive-only Panda adapter")
        self.ready()
        self.pub.send(self.serialize([(address, data, bus)], msgtype="sendcan"))

    def can_clear(self, flags):
        self.can_recv()

    def close(self):
        # Messaging sockets are released when this private adapter exits.
        pass


def serve():
    # The Go owner handles Ctrl-C and sends recovery traffic before closing us.
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    adapter = None
    passive = False
    try:
        for line in sys.stdin:
            close = False
            try:
                request = json.loads(line)
                op = request["op"]
                # Panda and dependent libraries may print diagnostics. Reserve
                # stdout exclusively for the machine protocol.
                with contextlib.redirect_stdout(sys.stderr):
                    if op == "status":
                        managed = pandad_running()
                        result = {"backend": "panda", "mode": "managed-sendcan" if managed else "direct-panda",
                                  "pandad_running": managed, "hardware_probed": False, "ready": True}
                        if managed:
                            if request.get("obd_multiplexing"):
                                raise RuntimeError("OBD bus-1 remapping requires direct Panda ownership; stop openpilot/manager first")
                            managed_states()
                            result["hardware_probed"] = True
                    elif op == "open":
                        if adapter is not None:
                            raise RuntimeError("Panda adapter already open")
                        passive = request.get("passive", False)
                        if pandad_running():
                            if request.get("obd_multiplexing"):
                                raise RuntimeError("OBD bus-1 remapping requires direct Panda ownership; stop openpilot/manager first")
                            adapter = Managed(passive)
                        else:
                            from panda import Panda
                            adapter = Panda()  # upstream USB/SPI selection and initialization
                            if not passive:
                                from opendbc.car.structs import CarParams
                                adapter.set_safety_mode(CarParams.SafetyModel.elm327, 0 if request.get("obd_multiplexing") else 1)
                        result = {}
                    elif op == "close":
                        close = True
                        result = {}
                    elif adapter is None:
                        raise RuntimeError("Panda adapter is not open")
                    elif op == "recv":
                        result = {"frames": [{"address": a, "data": d.hex(), "bus": b} for a, d, b in adapter.can_recv()]}
                    elif op == "send":
                        if passive:
                            raise RuntimeError("receive-only Panda adapter")
                        adapter.can_send(request["address"], bytes.fromhex(request["data"]), request["bus"])
                        result = {}
                    elif op == "clear":
                        adapter.can_clear(0xffff)
                        result = {}
                    else:
                        raise RuntimeError("unknown Panda adapter operation")
                print(json.dumps({"ok": True, **result}), flush=True)
            except Exception as exc:
                print(json.dumps({"ok": False, "error": str(exc)}), flush=True)
            if close:
                break
    finally:
        if adapter is not None:
            with contextlib.redirect_stdout(sys.stderr):
                adapter.close()


if __name__ == "__main__":
    serve()
