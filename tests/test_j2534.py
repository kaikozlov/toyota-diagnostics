from __future__ import annotations

import ctypes
import unittest
from unittest import mock

from toyota_diag import j2534, registry, transport


class FakeApi:
  def __init__(self, path: str, reads=()):
    self.path = path
    self.reads = list(reads)
    self.calls = []
    self.writes = []

  def open(self, selector=None):
    self.calls.append(("open", selector))
    return 11

  def read_version(self, device_id):
    self.calls.append(("read_version", device_id))
    return {"firmware": "fake-fw", "dll": "fake-dll", "api": "04.04"}

  def connect(self, device_id, protocol_id, flags, baud):
    self.calls.append(("connect", device_id, protocol_id, flags, baud))
    return 22

  def start_pass_all_filter(self, channel_id, *, extended=False):
    self.calls.append(("start_filter", channel_id, extended))
    return 34 if extended else 33

  def write(self, channel_id, msg, timeout_ms=1000):
    self.calls.append(("write", channel_id, timeout_ms))
    self.writes.append(msg)

  def read(self, channel_id, *, count=64, timeout_ms=0):
    self.calls.append(("read", channel_id, count, timeout_ms))
    return (j2534.STATUS_NOERROR, self.reads.pop(0)) if self.reads else (j2534.ERR_BUFFER_EMPTY, [])

  def stop_filter(self, channel_id, filter_id):
    self.calls.append(("stop_filter", channel_id, filter_id))

  def disconnect(self, channel_id):
    self.calls.append(("disconnect", channel_id))

  def close(self, device_id):
    self.calls.append(("close", device_id))


def passthru_msg(address: int, payload: bytes, *, rx_status=0):
  msg = j2534.PassThruMsg()
  msg.ProtocolID = j2534.PROTOCOL_CAN
  msg.RxStatus = rx_status
  data = address.to_bytes(4, "big") + payload
  msg.DataSize = len(data)
  for i, value in enumerate(data):
    msg.Data[i] = value
  return msg


class _FakeFunction:
  def __init__(self, callback=lambda *args: 0):
    self.callback = callback
    self.argtypes = None
    self.restype = None

  def __call__(self, *args):
    return self.callback(*args)


class _FakeLibrary:
  def __init__(self):
    self.filters = []
    names = [
      "PassThruOpen", "PassThruClose", "PassThruConnect", "PassThruDisconnect",
      "PassThruReadMsgs", "PassThruWriteMsgs", "PassThruStopMsgFilter", "PassThruReadVersion", "PassThruGetLastError",
    ]
    for name in names:
      setattr(self, name, _FakeFunction())

    def start_filter(channel, filter_type, mask_ptr, pattern_ptr, flow_ptr, filter_id_ptr):
      mask = ctypes.cast(mask_ptr, ctypes.POINTER(j2534.PassThruMsg)).contents
      pattern = ctypes.cast(pattern_ptr, ctypes.POINTER(j2534.PassThruMsg)).contents
      self.filters.append((int(channel), int(filter_type), int(mask.TxFlags), int(pattern.TxFlags)))
      ctypes.cast(filter_id_ptr, ctypes.POINTER(j2534.U32)).contents.value = 70 + len(self.filters)
      return 0

    self.PassThruStartMsgFilter = _FakeFunction(start_filter)


class TestJ2534(unittest.TestCase):
  @classmethod
  def setUpClass(cls):
    cls.profile = registry.load_registry(registry.LEGACY_CAMRY_REGISTRY)

  def test_passthru_msg_uses_fixed_32bit_v0404_layout(self):
    self.assertEqual(ctypes.sizeof(j2534.PassThruMsg), 24 + j2534.MAX_MSG_DATA)
    self.assertEqual(j2534.PassThruMsg.Data.offset, 24)

  def test_pass_filters_keep_11bit_and_29bit_id_types_separate(self):
    lib = _FakeLibrary()
    api = j2534.Api("fake", loader=lambda _: lib)
    standard = api.start_pass_all_filter(22, extended=False)
    extended = api.start_pass_all_filter(22, extended=True)
    self.assertEqual((standard, extended), (71, 72))
    self.assertEqual(lib.filters, [
      (22, j2534.PASS_FILTER, 0, 0),
      (22, j2534.PASS_FILTER, j2534.CAN_29BIT_ID, j2534.CAN_29BIT_ID),
    ])

  def test_explicit_provider_does_not_require_windows_registry(self):
    rows = j2534.discover_providers("/tmp/MVCI32.dll")
    self.assertEqual(rows, [j2534.Provider(name="MVCI32", library="/tmp/MVCI32.dll", source="explicit")])

  def test_can_adapter_opens_raw_can_both_id_mode_and_frames_tx(self):
    fake = FakeApi("/tmp/provider")
    with mock.patch("toyota_diag.j2534.resolve_provider", return_value=j2534.Provider("fake", "/tmp/provider", "test")):
      adapter = j2534.CanAdapter(
        library_path="/tmp/provider", device_selector="0403:6001", baud=500_000, bus=2,
        api_factory=lambda path: fake,
      )
    self.assertEqual(fake.calls[:5], [
      ("open", "0403:6001"),
      ("read_version", 11),
      ("connect", 11, j2534.PROTOCOL_CAN, j2534.CAN_ID_BOTH, 500_000),
      ("start_filter", 22, False),
      ("start_filter", 22, True),
    ])

    adapter.can_send(0x792, bytes.fromhex("0322160100000000"), 2)
    adapter.can_send(0x18DA00F1, bytes.fromhex("0322A10000000000"), 2)
    standard, extended = fake.writes
    self.assertEqual(bytes(standard.Data[:standard.DataSize]), bytes.fromhex("000007920322160100000000"))
    self.assertEqual(standard.TxFlags, 0)
    self.assertEqual(bytes(extended.Data[:extended.DataSize]), bytes.fromhex("18da00f10322a10000000000"))
    self.assertEqual(extended.TxFlags, j2534.CAN_29BIT_ID)
    adapter.close()
    self.assertEqual(fake.calls[-4:], [
      ("stop_filter", 22, 33), ("stop_filter", 22, 34), ("disconnect", 22), ("close", 11),
    ])

  def test_can_adapter_decodes_rx_and_discards_tx_indications(self):
    receive = passthru_msg(0x79A, bytes.fromhex("0762160100010000"))
    echo = passthru_msg(0x792, bytes.fromhex("0322160100000000"), rx_status=j2534.TX_MSG_TYPE)
    fake = FakeApi("/tmp/provider", reads=[[receive, echo]])
    with mock.patch("toyota_diag.j2534.resolve_provider", return_value=j2534.Provider("fake", "/tmp/provider", "test")):
      adapter = j2534.CanAdapter(library_path="/tmp/provider", bus=3, api_factory=lambda path: fake)
    self.assertEqual(adapter.can_recv(), [(0x79A, bytes.fromhex("0762160100010000"), 3)])
    adapter.close()

  def test_existing_opendbc_uds_stack_runs_over_j2534_raw_can(self):
    response = passthru_msg(0x79A, bytes.fromhex("0462123456000000"))
    # IsoTpMessage drains stale frames before TX, then reads the real response.
    fake = FakeApi("/tmp/provider", reads=[[], [response]])
    with mock.patch("toyota_diag.j2534.resolve_provider", return_value=j2534.Provider("fake", "/tmp/provider", "test")):
      adapter = j2534.CanAdapter(library_path="/tmp/provider", bus=self.profile.bus, api_factory=lambda path: fake)
    client = transport.uds_client_factory(adapter, self.profile, validate_profile_routes=False)(0x792)
    self.assertEqual(client.read_data_by_identifier(0x1234), b"\x56")
    self.assertEqual(len(fake.writes), 1)
    request = fake.writes[0]
    self.assertEqual(bytes(request.Data[:request.DataSize]), bytes.fromhex("000007920322123400000000"))
    adapter.close()

  def test_transport_facade_constructs_j2534_without_touching_panda(self):
    sentinel = object()
    with mock.patch("toyota_diag.j2534.CanAdapter", return_value=sentinel) as adapter, \
         mock.patch("toyota_diag.transport.pandad_running") as pandad:
      result = transport.connect(
        self.profile, backend="j2534", j2534_library="/tmp/provider", j2534_device="serial:/dev/cu.fake",
        j2534_baud=250_000,
      )
    self.assertIs(result, sentinel)
    pandad.assert_not_called()
    adapter.assert_called_once_with(
      library_path="/tmp/provider", device_selector="serial:/dev/cu.fake", baud=250_000, bus=self.profile.bus,
    )

  def test_j2534_rejects_panda_obd_multiplexing_option(self):
    with self.assertRaisesRegex(SystemExit, "Panda-only"):
      transport.connect(self.profile, backend="j2534", obd_multiplexing=True)


if __name__ == "__main__":
  unittest.main()
