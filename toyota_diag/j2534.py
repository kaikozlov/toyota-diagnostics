"""SAE J2534 v04.04 raw-CAN backend.

The first J2534 transport slice deliberately opens a raw CAN channel and reuses
opendbc's existing ISO-TP/UDS implementation above it. That keeps Toyota
operation semantics transport-neutral while supporting both ordinary 11-bit and
29-bit CAN diagnostics through a standard pass-thru VCI. Native J2534 ISO15765,
CAN-FD, K-Line, and DoIP remain separate future transport capabilities.
"""
from __future__ import annotations

import ctypes
import ctypes.util
from dataclasses import dataclass
import os
from pathlib import Path
import platform
from typing import Any, Callable

# J2534-1 v04.04 protocol/filter/connect constants.
PROTOCOL_CAN = 0x05
PASS_FILTER = 0x01
CAN_29BIT_ID = 0x00000100
CAN_ID_BOTH = 0x00000800

# RxStatus values used to suppress the adapter's own TX indications.
TX_MSG_TYPE = 0x00000001

STATUS_NOERROR = 0x00
ERR_NOT_SUPPORTED = 0x01
ERR_INVALID_FLAGS = 0x06
ERR_TIMEOUT = 0x09
ERR_BUFFER_EMPTY = 0x10

# OpenMVCI currently returns signed internal status values rather than the
# positive J2534 v04.04 values for these two empty-read states. Supporting them
# here costs nothing and makes the generic ABI usable with that implementation.
OPENMVCI_ERR_TIMEOUT = -6
OPENMVCI_ERR_BUFFER_EMPTY = -7
EMPTY_READ_STATUSES = frozenset({ERR_TIMEOUT, ERR_BUFFER_EMPTY, OPENMVCI_ERR_TIMEOUT, OPENMVCI_ERR_BUFFER_EMPTY})

MAX_MSG_DATA = 4128
CLASSIC_CAN_MAX_DATA = 8
DEFAULT_BAUD = 500_000
DEFAULT_WRITE_TIMEOUT_MS = 1_000
DEFAULT_READ_BATCH = 64

U32 = ctypes.c_uint32
I32 = ctypes.c_int32
U8 = ctypes.c_uint8


class PassThruMsg(ctypes.Structure):
  _fields_ = [
    ("ProtocolID", U32),
    ("RxStatus", U32),
    ("TxFlags", U32),
    ("Timestamp", U32),
    ("DataSize", U32),
    ("ExtraDataIndex", U32),
    ("Data", U8 * MAX_MSG_DATA),
  ]


@dataclass(frozen=True)
class Provider:
  name: str
  library: str
  source: str

  def document(self) -> dict[str, str]:
    return {"name": self.name, "library": self.library, "source": self.source}


class J2534Error(RuntimeError):
  def __init__(self, operation: str, status: int | None = None, detail: str | None = None) -> None:
    self.operation = operation
    self.status = status
    self.detail = detail
    suffix = ""
    if status is not None:
      suffix += f" status={status} ({status & 0xFFFFFFFF:#010x})"
    if detail:
      suffix += f": {detail}"
    super().__init__(f"J2534 {operation} failed{suffix}")


def _dedupe_providers(rows: list[Provider]) -> list[Provider]:
  out: list[Provider] = []
  seen: set[str] = set()
  for row in rows:
    key = os.path.normcase(os.path.abspath(os.path.expanduser(row.library))) if os.path.sep in row.library else row.library.casefold()
    if key in seen:
      continue
    seen.add(key)
    out.append(row)
  return out


def _windows_registry_providers() -> list[Provider]:
  if platform.system() != "Windows":
    return []
  try:
    import winreg
  except ImportError:
    return []

  rows: list[Provider] = []
  roots = (
    (r"SOFTWARE\PassThruSupport.04.04", 0),
    (r"SOFTWARE\PassThruSupport.04.04", getattr(winreg, "KEY_WOW64_32KEY", 0)),
  )
  for key_name, view in roots:
    try:
      root = winreg.OpenKey(winreg.HKEY_LOCAL_MACHINE, key_name, 0, winreg.KEY_READ | view)
    except OSError:
      continue
    try:
      index = 0
      while True:
        try:
          subkey_name = winreg.EnumKey(root, index)
        except OSError:
          break
        index += 1
        subkey = None
        try:
          subkey = winreg.OpenKey(root, subkey_name)
          library = str(winreg.QueryValueEx(subkey, "FunctionLibrary")[0])
          try:
            name = str(winreg.QueryValueEx(subkey, "Name")[0])
          except OSError:
            name = subkey_name
          rows.append(Provider(name=name or subkey_name, library=library, source="windows-registry"))
        except OSError:
          continue
        finally:
          if subkey is not None:
            try:
              subkey.Close()
            except OSError:
              pass
    finally:
      root.Close()
  return rows


def _openmvci_candidates() -> list[Provider]:
  system = platform.system()
  names: list[str] = []
  if system == "Darwin":
    names.extend((
      "/opt/homebrew/lib/libopenmvci.dylib",
      "/usr/local/lib/libopenmvci.dylib",
    ))
  elif system == "Linux":
    names.extend((
      "/usr/local/lib/libopenmvci.so",
      "/usr/lib/libopenmvci.so",
    ))

  found = ctypes.util.find_library("openmvci")
  if found:
    names.insert(0, found)

  rows: list[Provider] = []
  for value in names:
    # Bare loader names are useful even when they do not resolve to a filesystem
    # path in Python; absolute/common paths are only advertised if present.
    if os.path.isabs(value) and not Path(value).is_file():
      continue
    rows.append(Provider(name="OpenMVCI", library=value, source="system-search"))
  return rows


def discover_providers(explicit_library: str | None = None) -> list[Provider]:
  """Discover J2534 providers without opening hardware or transmitting."""
  if explicit_library:
    return [Provider(name=Path(explicit_library).stem or "J2534", library=explicit_library, source="explicit")]

  env_library = os.environ.get("TOYOTA_J2534_LIBRARY")
  rows: list[Provider] = []
  if env_library:
    rows.append(Provider(name=Path(env_library).stem or "J2534", library=env_library, source="environment"))
  rows.extend(_windows_registry_providers())
  rows.extend(_openmvci_candidates())
  return _dedupe_providers(rows)


def resolve_provider(explicit_library: str | None = None) -> Provider:
  rows = discover_providers(explicit_library)
  if not rows:
    raise J2534Error(
      "provider discovery", detail=(
        "no J2534 library found; pass --j2534-library PATH, set TOYOTA_J2534_LIBRARY, "
        "install a Windows PassThruSupport.04.04 provider, or install libopenmvci"
      ),
    )
  return rows[0]


def _default_loader(path: str):
  try:
    if platform.system() == "Windows":
      return ctypes.WinDLL(path)
    return ctypes.CDLL(path)
  except OSError as e:
    hint = ""
    if platform.system() == "Windows":
      hint = " (the Python process and J2534 DLL must have compatible architectures)"
    raise J2534Error("library load", detail=f"{path}: {e}{hint}") from e


def _bind(lib: Any, name: str, argtypes: list[Any], restype: Any = I32):
  try:
    fn = getattr(lib, name)
  except AttributeError as e:
    raise J2534Error("library load", detail=f"missing required export {name}") from e
  fn.argtypes = argtypes
  fn.restype = restype
  return fn


class Api:
  """Thin ctypes binding for the common J2534 v04.04 pass-thru ABI."""

  def __init__(self, library_path: str, *, loader: Callable[[str], Any] = _default_loader) -> None:
    self.library_path = library_path
    self.lib = loader(library_path)
    msg_p = ctypes.POINTER(PassThruMsg)
    u32_p = ctypes.POINTER(U32)
    char_p = ctypes.POINTER(ctypes.c_char)
    self._open = _bind(self.lib, "PassThruOpen", [ctypes.c_char_p, u32_p])
    self._close = _bind(self.lib, "PassThruClose", [U32])
    self._connect = _bind(self.lib, "PassThruConnect", [U32, U32, U32, U32, u32_p])
    self._disconnect = _bind(self.lib, "PassThruDisconnect", [U32])
    self._read = _bind(self.lib, "PassThruReadMsgs", [U32, msg_p, u32_p, U32])
    self._write = _bind(self.lib, "PassThruWriteMsgs", [U32, msg_p, u32_p, U32])
    self._start_filter = _bind(self.lib, "PassThruStartMsgFilter", [U32, U32, msg_p, msg_p, msg_p, u32_p])
    self._stop_filter = _bind(self.lib, "PassThruStopMsgFilter", [U32, U32])
    self._read_version = _bind(self.lib, "PassThruReadVersion", [U32, char_p, char_p, char_p])
    self._last_error = _bind(self.lib, "PassThruGetLastError", [char_p])

  @staticmethod
  def _status(value: Any) -> int:
    return int(value)

  def last_error(self) -> str | None:
    buf = ctypes.create_string_buffer(256)
    try:
      status = self._status(self._last_error(buf))
    except Exception:
      return None
    if status != STATUS_NOERROR:
      return None
    return buf.value.decode("utf-8", errors="replace") or None

  def check(self, status: int, operation: str, *, allow: frozenset[int] = frozenset()) -> int:
    if status == STATUS_NOERROR or status in allow:
      return status
    raise J2534Error(operation, status, self.last_error())

  def open(self, device_selector: str | None = None) -> int:
    device_id = U32()
    selector = None if device_selector is None else device_selector.encode("utf-8")
    self.check(self._status(self._open(selector, ctypes.byref(device_id))), "PassThruOpen")
    return int(device_id.value)

  def close(self, device_id: int) -> None:
    self.check(self._status(self._close(device_id)), "PassThruClose")

  def connect(self, device_id: int, protocol_id: int, flags: int, baud: int) -> int:
    channel_id = U32()
    self.check(
      self._status(self._connect(device_id, protocol_id, flags, baud, ctypes.byref(channel_id))),
      "PassThruConnect",
    )
    return int(channel_id.value)

  def disconnect(self, channel_id: int) -> None:
    self.check(self._status(self._disconnect(channel_id)), "PassThruDisconnect")

  def write(self, channel_id: int, msg: PassThruMsg, timeout_ms: int = DEFAULT_WRITE_TIMEOUT_MS) -> None:
    count = U32(1)
    status = self._status(self._write(channel_id, ctypes.byref(msg), ctypes.byref(count), timeout_ms))
    self.check(status, "PassThruWriteMsgs")
    if count.value != 1:
      raise J2534Error("PassThruWriteMsgs", status, f"provider reported {count.value} of 1 messages written")

  def read(self, channel_id: int, *, count: int = DEFAULT_READ_BATCH, timeout_ms: int = 0) -> tuple[int, list[PassThruMsg]]:
    if count <= 0:
      return STATUS_NOERROR, []
    messages = (PassThruMsg * count)()
    actual = U32(count)
    status = self._status(self._read(channel_id, messages, ctypes.byref(actual), timeout_ms))
    rows = [messages[index] for index in range(min(int(actual.value), count))]
    if status != STATUS_NOERROR and status not in EMPTY_READ_STATUSES:
      self.check(status, "PassThruReadMsgs")
    return status, rows

  def start_pass_all_filter(self, channel_id: int, *, extended: bool = False) -> int:
    mask = PassThruMsg()
    pattern = PassThruMsg()
    flags = CAN_29BIT_ID if extended else 0
    for msg in (mask, pattern):
      msg.ProtocolID = PROTOCOL_CAN
      msg.TxFlags = flags
      msg.DataSize = 4
    filter_id = U32()
    self.check(
      self._status(self._start_filter(channel_id, PASS_FILTER, ctypes.byref(mask), ctypes.byref(pattern), None, ctypes.byref(filter_id))),
      "PassThruStartMsgFilter",
    )
    return int(filter_id.value)

  def stop_filter(self, channel_id: int, filter_id: int) -> None:
    self.check(self._status(self._stop_filter(channel_id, filter_id)), "PassThruStopMsgFilter")

  def read_version(self, device_id: int) -> dict[str, str]:
    firmware = ctypes.create_string_buffer(80)
    dll = ctypes.create_string_buffer(80)
    api = ctypes.create_string_buffer(80)
    self.check(self._status(self._read_version(device_id, firmware, dll, api)), "PassThruReadVersion")
    return {
      "firmware": firmware.value.decode("utf-8", errors="replace"),
      "dll": dll.value.decode("utf-8", errors="replace"),
      "api": api.value.decode("utf-8", errors="replace"),
    }


def _message(protocol: int, data: bytes, *, tx_flags: int = 0) -> PassThruMsg:
  if len(data) > MAX_MSG_DATA:
    raise ValueError(f"J2534 message payload exceeds {MAX_MSG_DATA} bytes")
  msg = PassThruMsg()
  msg.ProtocolID = protocol
  msg.TxFlags = tx_flags
  msg.DataSize = len(data)
  for index, value in enumerate(data):
    msg.Data[index] = value
  return msg


def _can_message(address: int, data: bytes) -> PassThruMsg:
  if not 0 <= address <= 0x1FFFFFFF:
    raise ValueError(f"CAN address does not fit 29 bits: {address:#x}")
  if len(data) > CLASSIC_CAN_MAX_DATA:
    raise ValueError(f"J2534 raw classic-CAN backend supports at most {CLASSIC_CAN_MAX_DATA} data bytes")
  flags = CAN_29BIT_ID if address > 0x7FF else 0
  return _message(PROTOCOL_CAN, address.to_bytes(4, "big") + bytes(data), tx_flags=flags)


class CanAdapter:
  """Panda-shaped raw-CAN adapter backed by a J2534 v04.04 provider."""

  def __init__(self, *, library_path: str | None = None, device_selector: str | None = None,
               baud: int = DEFAULT_BAUD, bus: int = 0, api_factory: Callable[[str], Api] = Api) -> None:
    if baud <= 0:
      raise ValueError("J2534 baud must be positive")
    self.provider = resolve_provider(library_path)
    self.device_selector = device_selector or os.environ.get("TOYOTA_J2534_DEVICE")
    self.baud = baud
    self.bus = bus
    self.api = api_factory(self.provider.library)
    self.device_id: int | None = None
    self.channel_id: int | None = None
    self.filter_ids: list[int] = []
    self.version: dict[str, str] | None = None
    try:
      self.device_id = self.api.open(self.device_selector)
      try:
        self.version = self.api.read_version(self.device_id)
      except J2534Error:
        self.version = None
      # CAN_ID_BOTH lets one raw channel carry Toyota's ordinary 11-bit routes
      # plus the P6 normal-fixed 29-bit routes. The per-message TxFlags still
      # identifies each extended CAN frame.
      self.channel_id = self.api.connect(self.device_id, PROTOCOL_CAN, CAN_ID_BOTH, self.baud)
      self.filter_ids = [
        self.api.start_pass_all_filter(self.channel_id, extended=False),
        self.api.start_pass_all_filter(self.channel_id, extended=True),
      ]
    except BaseException:
      self._best_effort_close()
      raise

  def can_send(self, address: int, data: bytes, bus: int, timeout=None) -> None:
    del bus
    if self.channel_id is None:
      raise J2534Error("CAN write", detail="adapter is closed")
    timeout_ms = DEFAULT_WRITE_TIMEOUT_MS
    if timeout is not None:
      # Panda's timeout argument is not consistently used by upstream callers;
      # preserve sensible millisecond values without making transport timing a
      # semantic input to Toyota operations.
      try:
        candidate = int(timeout)
        if candidate > 0:
          timeout_ms = candidate
      except (TypeError, ValueError):
        pass
    self.api.write(self.channel_id, _can_message(address, data), timeout_ms)

  def can_recv(self) -> list[tuple[int, bytes, int]]:
    if self.channel_id is None:
      return []
    _, messages = self.api.read(self.channel_id, count=DEFAULT_READ_BATCH, timeout_ms=0)
    frames: list[tuple[int, bytes, int]] = []
    for msg in messages:
      if int(msg.ProtocolID) != PROTOCOL_CAN or int(msg.RxStatus) & TX_MSG_TYPE or int(msg.DataSize) < 4:
        continue
      raw = bytes(msg.Data[:int(msg.DataSize)])
      address = int.from_bytes(raw[:4], "big") & 0x1FFFFFFF
      frames.append((address, raw[4:], self.bus))
    return frames

  def can_clear(self, flags: int) -> None:
    del flags
    # Drain instead of relying on CLEAR_RX_BUFFER: v04.04 providers and
    # OpenMVCI currently disagree on some IOCTL numeric values, while reads are
    # part of the stable common ABI.
    for _ in range(32):
      if not self.can_recv():
        return

  def close(self) -> None:
    errors: list[Exception] = []
    if self.channel_id is not None and self.filter_ids:
      for filter_id in self.filter_ids:
        try:
          self.api.stop_filter(self.channel_id, filter_id)
        except Exception as e:
          errors.append(e)
      self.filter_ids = []
    if self.channel_id is not None:
      try:
        self.api.disconnect(self.channel_id)
      except Exception as e:
        errors.append(e)
      self.channel_id = None
    if self.device_id is not None:
      try:
        self.api.close(self.device_id)
      except Exception as e:
        errors.append(e)
      self.device_id = None
    if errors:
      raise errors[0]

  def _best_effort_close(self) -> None:
    try:
      self.close()
    except Exception:
      pass

  def __enter__(self):
    return self

  def __exit__(self, exc_type, exc, tb):
    self.close()
    return False

  def __del__(self):
    self._best_effort_close()


def provider_documents(explicit_library: str | None = None) -> list[dict[str, Any]]:
  rows: list[dict[str, Any]] = []
  for provider in discover_providers(explicit_library):
    row: dict[str, Any] = provider.document()
    try:
      Api(provider.library)
      row.update(loadable=True, error=None)
    except J2534Error as e:
      row.update(loadable=False, error=str(e))
    rows.append(row)
  return rows


def status(*, library_path: str | None = None, device_selector: str | None = None,
           baud: int = DEFAULT_BAUD) -> dict[str, Any]:
  """Load-check a provider without opening the VCI or transmitting."""
  try:
    provider = resolve_provider(library_path)
    Api(provider.library)
  except J2534Error as e:
    return {
      "backend": "j2534", "mode": "j2534-raw-can", "ready": False,
      "hardware_probed": False, "library": library_path, "device_selector": device_selector,
      "baud": baud, "detail": str(e),
    }
  return {
    "backend": "j2534", "mode": "j2534-raw-can", "ready": True,
    "hardware_probed": False, "provider": provider.name, "provider_source": provider.source,
    "library": provider.library, "device_selector": device_selector or os.environ.get("TOYOTA_J2534_DEVICE"),
    "baud": baud,
    "detail": "J2534 provider loaded; hardware is opened only by a live command",
  }
