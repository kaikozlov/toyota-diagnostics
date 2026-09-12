"""Generic operation executors for recovered direct (0x2F) and routine (0x31) Active Tests.

Static execution remains fail-closed: fixed routines require fully materialized
request geometry, while direct tests require an exact payload width. Current P5
direct tests with an exported runtime-length probe may materialize that one missing
fact after explicit execution acknowledgement by issuing Toyota's recovered
selector-0xCA support read (`22 <DID>`). GTS+ stores `received_length - 3` in
`DataIdLengthList`; the shared UDS client returns the already-stripped value
bytes, so their length is the same exact N. Ambiguous rows and every other
unresolved input remain non-executable. No option record, session requirement, or request geometry is
guessed from partial data.

Run backends are explicit-by-default: `execute=False` (the default) performs a
plan-only echo with no transmission. When execution is acknowledged, the backend
enters Toyota's recovered extended session automatically when the row's
`session_requirement` is `extended`. Started operations are always stopped —
including on exception and KeyboardInterrupt — before the error propagates; the
caller owns the `DiagnosticSession` context so session cleanup happens after the
result is returned.
"""
from __future__ import annotations

import time
from decimal import Decimal, InvalidOperation
from collections.abc import Callable
from dataclasses import dataclass
from typing import Any

from opendbc.car.uds import CONTROL_PARAMETER_TYPE, ROUTINE_CONTROL_TYPE

from toyota_diag import registry
from toyota_diag.registry import EcuSpec
from toyota_diag.session import DiagnosticSession, LifecycleError, parse_lifecycle

EXECUTION_EXECUTABLE = "executable"
SESSION_REQUIREMENT_EXTENDED = "extended"
SESSION_REQUIREMENT_DEFAULT = "default"
SESSION_REQUIREMENT_NONE = "none"
DECLARED_SESSION_REQUIREMENTS = frozenset({SESSION_REQUIREMENT_EXTENDED, SESSION_REQUIREMENT_DEFAULT,
                                           SESSION_REQUIREMENT_NONE})

DIRECT_SERVICE = 0x2F
DIRECT_POSITIVE_SID = 0x6F
ROUTINE_SERVICE = 0x31
ROUTINE_POSITIVE_SID = 0x71
ROUTINE_CONTROL_START = int(ROUTINE_CONTROL_TYPE.START)
ROUTINE_CONTROL_STOP = int(ROUTINE_CONTROL_TYPE.STOP)
ROUTINE_CONTROL_RESULTS = int(ROUTINE_CONTROL_TYPE.REQUEST_RESULTS)
DIRECT_CONTROLS = frozenset(int(control) for control in CONTROL_PARAMETER_TYPE)


class ExecutorError(ValueError):
  """Invalid executor use (wrong kind, missing runtime data, bad lengths)."""


class PlanNotExecutable(ExecutorError):
  """The registry row does not resolve to an executable plan."""

  def __init__(self, plan: TestPlan, refusals: tuple[str, ...] | None = None) -> None:
    self.plan = plan
    self.refusals = plan.refusals if refusals is None else refusals
    details = "; ".join(self.refusals) if self.refusals else "plan is not resolved for execution"
    super().__init__(f"{plan.describe()} is not executable: {details}")


@dataclass(frozen=True, kw_only=True)
class TestPlan:
  """Common resolved-plan shape; `executable` is False unless every refusal is empty."""
  ecu: EcuSpec
  test_id: int
  name: str
  kind: str
  session_requirement: str | None = None
  positive_sid: int = 0
  refusals: tuple[str, ...] = ()

  @property
  def executable(self) -> bool:
    return not self.refusals

  def describe(self) -> str:
    return f"{self.ecu.key} 0x{self.test_id:04X} {self.name!r} ({self.kind})"


@dataclass(frozen=True, kw_only=True)
class DirectTestPlan(TestPlan):
  """Recovered 0x2F InputOutputControlByIdentifier plan.

  start/stop prefixes are decomposed as `2F | DID | control | option prefix`.
  The runtime DID option record has exact length N; start/stop control-enable masks
  are separate, encoding-mode-specific geometry recovered from Toyota type-67/type-68.
  """
  did: int = 0
  start_control: int = 0
  start_option_prefix: bytes = b""
  stop_control: int = 0
  stop_option_prefix: bytes = b""
  runtime_length: int | None = None


@dataclass(frozen=True, kw_only=True)
class MultiDirectTestPlan(TestPlan):
  """Current P5 type-33 group composed from ordinary direct member controls."""
  did: int = 0
  member_ids: tuple[int, ...] = ()
  input_slots: tuple[int, ...] = ()
  member_rows: tuple[dict[str, Any], ...] = ()
  runtime_length: int | None = None


@dataclass(frozen=True, kw_only=True)
class RoutineTestPlan(TestPlan):
  """Recovered 0x31 RoutineControl plan with start/status/stop phases."""
  rid: int = 0
  start_control: int = ROUTINE_CONTROL_START
  start_option_prefix: bytes = b""
  stop_control: int = ROUTINE_CONTROL_STOP
  stop_option_prefix: bytes = b""
  status_control: int | None = None
  status_option_prefix: bytes = b""
  parameterized: bool = False


@dataclass(frozen=True)
class ActiveTestResult:
  plan: TestPlan
  executed: bool
  session_requirement: str | None = None
  start: bytes | None = None
  statuses: tuple[tuple[float, bytes], ...] = ()
  stop: bytes | None = None
  cleanup_errors: tuple[str, ...] = ()


# -- plan resolution -----------------------------------------------------------------------------
def _base_refusals(row: dict[str, Any], kind: str, service: int, positive_sid: int) -> list[str]:
  refusals: list[str] = []
  if row.get("kind") != kind:
    refusals.append(f"kind is {row.get('kind')!r}, expected {kind!r}")
  if row.get("execution") != EXECUTION_EXECUTABLE:
    reason = row.get("reason") or row.get("error") or "runtime request geometry is not fully materialized"
    refusals.append(f"execution is {row.get('execution')!r}, not {EXECUTION_EXECUTABLE!r} ({reason})")
  if row.get("service") != service:
    refusals.append(f"service is {row.get('service')!r}, expected {service:#04x}")
  if row.get("positive_response") != positive_sid:
    refusals.append(f"positive response SID is {row.get('positive_response')!r}, expected {positive_sid:#04x}")
  if kind == "direct" and row.get("multi_control_group"):
    refusals.append(
      "selected Active Test is a type-33 multi-control group parent; use the grouped composer")
  if row.get("session_requirement") not in DECLARED_SESSION_REQUIREMENTS:
    declared = ", ".join(sorted(DECLARED_SESSION_REQUIREMENTS))
    refusals.append(f"session_requirement {row.get('session_requirement')!r} is not one of {declared}")
  return refusals


def _decompose_direct_prefix(prefix: bytes, did: int, what: str) -> tuple[int, bytes] | str:
  if len(prefix) < 4 or prefix[0] != DIRECT_SERVICE or int.from_bytes(prefix[1:3], "big") != did:
    return f"{what} {prefix.hex()} is not a resolved {DIRECT_SERVICE:02X} request for DID {did:04X}"
  control = prefix[3]
  if control not in DIRECT_CONTROLS:
    return f"{what} control parameter {control:#04x} is not a known control type"
  return control, prefix[4:]


def _decompose_routine_static(static: bytes, control: int, rid: int, what: str) -> bytes | str:
  if len(static) < 4 or static[0] != ROUTINE_SERVICE or static[1] != control or static[2:4] != rid.to_bytes(2, "big"):
    return f"{what} {static.hex()} is not a resolved {ROUTINE_SERVICE:02X} {control:02X} {rid:04X} request"
  return static[4:]


def resolve_plan(ecu: EcuSpec, row: dict[str, Any]) -> TestPlan:
  """Resolve a registry Active-Test/utility row into a validated plan; never transmits.

  Unresolved or opaque rows resolve with `executable=False` and their refusal
  reasons instead of raising, so plan/list UX can show them.
  """
  kind = row.get("kind")
  name = str(row.get("name") or "")
  try:
    test_id = registry.parse_int(row.get("id", 0), "Active Test id")
  except registry.RegistryError as e:
    return TestPlan(ecu=ecu, test_id=0, name=name, kind=kind or "", refusals=(f"malformed id: {e}",))
  if kind == "direct":
    return _resolve_direct(ecu, test_id, name, row)
  if kind == "routine":
    return _resolve_routine(ecu, test_id, name, row)
  return TestPlan(ecu=ecu, test_id=test_id, name=name, kind=kind or "",
                  refusals=(f"kind is {kind!r}, expected 'direct' or 'routine'",))


def _direct_runtime_length_probe(row: dict[str, Any], plan: TestPlan) -> dict[str, Any] | None:
  """Return a validated exact Toyota runtime-length probe, without transmitting."""
  if not isinstance(plan, DirectTestPlan) or plan.runtime_length is not None:
    return None
  if (row.get("kind") != "direct" or row.get("execution") not in {"plan_only", "executable"}
      or row.get("multi_control_group")):
    return None

  probe = row.get("runtime_length_probe")
  if isinstance(probe, dict):
    try:
      kind = str(probe.get("kind") or "")
      selector = registry.parse_int(probe.get("selector"), "runtime_length_probe.selector")
      request = registry.parse_bytes(probe.get("request"), "runtime_length_probe.request")
      check = registry.parse_bytes(probe.get("check"), "runtime_length_probe.check")
      prefix_length = registry.parse_int(probe.get("response_prefix_length"), "runtime_length_probe.response_prefix_length")
    except registry.RegistryError:
      return None
    if (kind == "read_data_by_identifier_value_length" and selector == 0xCA
        and request == bytes([0x22]) + plan.did.to_bytes(2, "big")
        and check == b"\x62" and prefix_length == 3):
      return dict(probe)
    return None

  # Backward compatibility for registry/bundle revisions produced before the
  # explicit probe descriptor was exported. Role-0x08 mode-0 used the same
  # selector-0xCA request, so this is still an exact recovered transaction.
  initial = row.get("initial_read")
  if not isinstance(initial, dict):
    return None
  try:
    if registry.parse_int(initial.get("mode"), "initial_read.mode") != 0:
      return None
    request = registry.parse_bytes(initial.get("request"), "initial_read.request")
    check = registry.parse_bytes(initial.get("check"), "initial_read.check")
  except registry.RegistryError:
    return None
  if request != bytes([0x22]) + plan.did.to_bytes(2, "big") or check != b"\x62":
    return None
  return {
    "kind": "read_data_by_identifier_value_length",
    "selector": "0xCA",
    "request": request.hex(),
    "check": check.hex(),
    "response_prefix_length": 3,
  }


def can_materialize_direct_runtime_length(row: dict[str, Any], plan: TestPlan) -> bool:
  """Return whether Toyota's recovered selector-0xCA probe can supply direct-test N.

  Current P5 `CheckSupportDid` builds `CCmdDataIdLengthList` from `22 <DID>`:
  N is the received UDS payload length minus the three-byte `62 <DID>` echo.
  `UdsClient.read_data_by_identifier()` already strips that echo, so the returned
  value length is exactly N. The predicate is pure and zero-transmit.
  """
  return _direct_runtime_length_probe(row, plan) is not None


def materialize_direct_runtime_length(session: DiagnosticSession, row: dict[str, Any],
                                      plan: DirectTestPlan) -> DirectTestPlan:
  """Materialize Toyota's live direct-test payload length from its exact support probe.

  The caller must have explicit mutation acknowledgement before invoking this: the
  function performs the recovered read-only selector-0xCA transaction and may enter
  the row's recovered diagnostic session. No guessed length is ever substituted.
  """
  probe = _direct_runtime_length_probe(row, plan)
  if probe is None:
    raise PlanNotExecutable(plan)

  # Prove that N is the only missing piece before touching the vehicle. A
  # temporary positive length is used only for local shape validation; it is
  # never transmitted and never returned.
  minimum_raw = row.get("runtime_length_minimum")
  try:
    minimum = registry.parse_int(minimum_raw, "runtime_length_minimum") if minimum_raw is not None else 1
  except registry.RegistryError as e:
    raise ExecutorError(f"malformed runtime-length minimum: {e}") from e
  if minimum <= 0:
    raise ExecutorError(f"runtime_length_minimum must be positive, got {minimum}")
  shape_row = dict(row)
  shape_row["execution"] = EXECUTION_EXECUTABLE
  shape_row["runtime_length"] = minimum
  shape_plan = resolve_plan(plan.ecu, shape_row)
  shape_refusals = runtime_refusals(session.profile, shape_plan)
  if shape_refusals:
    raise PlanNotExecutable(plan, shape_refusals)

  if plan.session_requirement == SESSION_REQUIREMENT_EXTENDED:
    session.enter_extended()
  data = session.client().read_data_by_identifier(plan.did)
  length = len(data)
  if length < minimum:
    raise ExecutorError(
      f"Toyota runtime-length probe DID 0x{plan.did:04X} returned {length} data byte(s), "
      f"below the recovered static minimum {minimum}")
  if length <= 0:
    raise ExecutorError(f"Toyota runtime-length probe DID 0x{plan.did:04X} returned no data bytes")

  live_row = dict(row)
  live_row["execution"] = EXECUTION_EXECUTABLE
  live_row["runtime_length"] = length
  live_plan = resolve_plan(plan.ecu, live_row)
  refusals = runtime_refusals(session.profile, live_plan)
  if refusals:
    raise PlanNotExecutable(live_plan, refusals)
  if not isinstance(live_plan, DirectTestPlan):
    raise ExecutorError("runtime materialization did not resolve a direct Active Test plan")
  return live_plan


def resolve_multi_direct_group(
    profile: registry.Profile,
    ecu: EcuSpec,
    group: dict[str, Any],
) -> MultiDirectTestPlan:
  refusals: list[str] = []
  try:
    group_id = registry.parse_int(group["group_id"], "multi group id")
  except (KeyError, registry.RegistryError) as e:
    return MultiDirectTestPlan(ecu=ecu, test_id=0, name=str(group.get("name") or ""), kind="multi_direct",
                               refusals=(f"malformed multi group id: {e}",))
  name = str(group.get("name") or "")
  if group.get("execution") != "materializable":
    refusals.append(str(group.get("reason") or "group is not materializable by the recovered current composer"))
  if group.get("composer") != "or_member_start_stop_frames":
    refusals.append(f"unsupported multi composer {group.get('composer')!r}")
  try:
    did = registry.parse_int(group["did"], "multi group DID")
  except (KeyError, registry.RegistryError) as e:
    did = 0
    refusals.append(f"multi group has no single recovered DID: {e}")

  member_inputs = group.get("member_inputs")
  if not isinstance(member_inputs, list) or not member_inputs:
    refusals.append("multi group has no exported member inputs")
    member_inputs = []
  member_rows: list[dict[str, Any]] = []
  member_ids: list[int] = []
  input_slots: list[int] = []
  session_requirements: set[str] = set()
  for entry in member_inputs:
    if not isinstance(entry, dict):
      refusals.append("multi group contains malformed member input metadata")
      continue
    try:
      member_id = registry.parse_int(entry["active_test_id"], "multi member id")
      input_slot = registry.parse_int(entry["input_slot"], "multi input slot")
      member_did = registry.parse_int(entry["did"], "multi member DID")
      member = profile.lookup_active_test(ecu, str(member_id), "direct")
    except (KeyError, registry.RegistryError) as e:
      refusals.append(f"cannot resolve multi member: {e}")
      continue
    if input_slot not in {1, 2}:
      refusals.append(f"multi member 0x{member_id:X} uses unsupported input slot {input_slot}")
    if did and member_did != did:
      refusals.append(f"multi member 0x{member_id:X} DID 0x{member_did:04X} != group DID 0x{did:04X}")
    if int(member.get("did", -1)) != member_did:
      refusals.append(f"multi member 0x{member_id:X} registry DID disagrees with group metadata")
    member_plan = resolve_plan(ecu, member)
    if not isinstance(member_plan, DirectTestPlan):
      refusals.append(f"multi member 0x{member_id:X} does not resolve as a direct plan")
    elif not can_materialize_direct_runtime_length(member, member_plan):
      refusals.append(f"multi member 0x{member_id:X} has no exact current runtime-length probe")
    requirement = member.get("session_requirement")
    if requirement in DECLARED_SESSION_REQUIREMENTS:
      session_requirements.add(str(requirement))
    else:
      refusals.append(f"multi member 0x{member_id:X} has invalid session requirement {requirement!r}")
    member_ids.append(member_id)
    input_slots.append(input_slot)
    member_rows.append(member)
  if len(set(member_ids)) != len(member_ids):
    refusals.append("multi group contains duplicate member IDs")
  if len(set(input_slots)) != len(input_slots):
    refusals.append("multi group contains duplicate input slots")
  if len(session_requirements) > 1:
    refusals.append("multi members disagree on session requirement")
  session_requirement = next(iter(session_requirements), None)
  # N is the one live fact intentionally absent from the static group plan.
  if not refusals:
    refusals.append("runtime payload length not definitively recovered")
  return MultiDirectTestPlan(
    ecu=ecu, test_id=group_id, name=name, kind="multi_direct",
    session_requirement=session_requirement, positive_sid=DIRECT_POSITIVE_SID,
    refusals=tuple(refusals), did=did, member_ids=tuple(member_ids), input_slots=tuple(input_slots),
    member_rows=tuple(member_rows), runtime_length=None,
  )


def can_materialize_multi_direct_runtime_length(plan: TestPlan) -> bool:
  return (
    isinstance(plan, MultiDirectTestPlan)
    and plan.runtime_length is None
    and plan.refusals == ("runtime payload length not definitively recovered",)
    and bool(plan.member_rows)
  )


def materialize_multi_direct_runtime_length(
    session: DiagnosticSession,
    plan: MultiDirectTestPlan,
) -> MultiDirectTestPlan:
  if not can_materialize_multi_direct_runtime_length(plan):
    raise PlanNotExecutable(plan)
  if plan.session_requirement == SESSION_REQUIREMENT_EXTENDED:
    session.enter_extended()
  data = session.client().read_data_by_identifier(plan.did)
  length = len(data)
  if length <= 0:
    raise ExecutorError(f"Toyota multi-control runtime-length probe DID 0x{plan.did:04X} returned no data bytes")
  for member in plan.member_rows:
    minimum_raw = member.get("runtime_length_minimum")
    minimum = registry.parse_int(minimum_raw, "runtime_length_minimum") if minimum_raw is not None else 1
    if length < minimum:
      raise ExecutorError(
        f"Toyota multi-control runtime-length probe DID 0x{plan.did:04X} returned {length} byte(s), "
        f"below member 0x{int(member['id']):X} minimum {minimum}")
  return MultiDirectTestPlan(
    ecu=plan.ecu, test_id=plan.test_id, name=plan.name, kind=plan.kind,
    session_requirement=plan.session_requirement, positive_sid=plan.positive_sid, refusals=(),
    did=plan.did, member_ids=plan.member_ids, input_slots=plan.input_slots,
    member_rows=plan.member_rows, runtime_length=length,
  )


def _or_bytes(current: bytearray, incoming: bytes) -> bytearray:
  if len(current) < len(incoming):
    current.extend(b"\x00" * (len(incoming) - len(current)))
  for index, value in enumerate(incoming):
    current[index] |= value
  return current


def compose_multi_direct_payload(
    plan: MultiDirectTestPlan,
    raw_values: dict[int, int],
) -> tuple[bytes, bytes, bytes]:
  """Reproduce FUN_10014440's per-member materialize + bytewise OR composition."""
  if plan.runtime_length is None or plan.refusals:
    raise PlanNotExecutable(plan)
  expected = set(plan.member_ids)
  supplied = set(raw_values)
  if supplied != expected:
    missing = sorted(expected - supplied)
    extra = sorted(supplied - expected)
    raise ExecutorError(f"multi group values mismatch; missing={missing}, extra={extra}")
  payload = bytearray(plan.runtime_length)
  start_mask = bytearray()
  stop_mask = bytearray()
  for member in plan.member_rows:
    member_id = int(member["id"])
    if int(member.get("did", -1)) != plan.did:
      raise ExecutorError(
        f"multi composer DID mismatch for member 0x{member_id:X}: 0x{int(member.get('did', 0)):04X} != 0x{plan.did:04X}")
    part_payload = pack_direct_raw_value(member, plan.runtime_length, int(raw_values[member_id]))
    part_start_mask, part_stop_mask = direct_control_enable_masks(member, plan.runtime_length)
    _or_bytes(payload, part_payload)
    _or_bytes(start_mask, part_start_mask)
    _or_bytes(stop_mask, part_stop_mask)
  return bytes(payload), bytes(start_mask), bytes(stop_mask)


def _routine_runtime_masks(row: dict[str, Any], plan: TestPlan) -> tuple[bytes, bytes, int] | None:
  """Validate GTS+'s current P5 routine runtime-mask geometry; zero-transmit."""
  if not isinstance(plan, RoutineTestPlan) or not plan.parameterized:
    return None
  if row.get("kind") != "routine" or row.get("execution") not in {"plan_only", "executable"}:
    return None
  try:
    value_meta = row.get("output_mask_value")
    button_meta = row.get("output_mask_button")
    if not isinstance(value_meta, dict) or not isinstance(button_meta, dict):
      return None
    value_text = value_meta.get("bytes", "")
    button_text = button_meta.get("bytes", "")
    value_mask = b"" if value_text in (None, "") else registry.parse_bytes(value_text, "routine output_mask_value")
    button_mask = b"" if button_text in (None, "") else registry.parse_bytes(button_text, "routine output_mask_button")
  except registry.RegistryError:
    return None
  static = plan.start_option_prefix
  width = len(static) or len(value_mask) or len(button_mask)
  if width <= 0 or (not value_mask and not button_mask):
    return None
  if any(len(part) not in (0, width) for part in (static, value_mask, button_mask)):
    return None
  return value_mask, button_mask, width


def can_materialize_routine_runtime(row: dict[str, Any], plan: TestPlan) -> bool:
  """Return whether exact exported GTS masks can materialize a routine option record."""
  return _routine_runtime_masks(row, plan) is not None


def materialize_routine_runtime(
    row: dict[str, Any],
    plan: RoutineTestPlan,
    *,
    value_payload: bytes | None = None,
    button_payload: bytes | None = None,
) -> RoutineTestPlan:
  """Merge explicit positional runtime bytes through Toyota's recovered routine masks.

  DataMonitorPhase5 starts with the static routine-command bytes and ORs caller/UI
  bytes only where the exported value/button masks permit them. This helper accepts
  already-encoded positional bytes; it does not guess GTS UI/physical-value encoding.
  """
  spec = _routine_runtime_masks(row, plan)
  if spec is None:
    raise PlanNotExecutable(plan)
  value_mask, button_mask, width = spec

  if value_mask:
    if value_payload is None:
      raise ExecutorError(f"routine requires --value with exactly {width} positional byte(s)")
    if len(value_payload) != width:
      raise ExecutorError(f"routine --value must be exactly {width} byte(s), got {len(value_payload)}")
  elif value_payload is not None:
    raise ExecutorError("routine has no value mask and does not accept --value")

  if button_mask:
    if button_payload is None:
      raise ExecutorError(f"routine requires --button with exactly {width} positional byte(s)")
    if len(button_payload) != width:
      raise ExecutorError(f"routine --button must be exactly {width} byte(s), got {len(button_payload)}")
  elif button_payload is not None:
    raise ExecutorError("routine has no button mask and does not accept --button")

  merged = bytearray(width)
  merged[:len(plan.start_option_prefix)] = plan.start_option_prefix
  if value_mask and value_payload is not None:
    for index, mask in enumerate(value_mask):
      merged[index] |= value_payload[index] & mask
  if button_mask and button_payload is not None:
    for index, mask in enumerate(button_mask):
      merged[index] |= button_payload[index] & mask

  live_row = dict(row)
  live_row["execution"] = EXECUTION_EXECUTABLE
  live_row["fixed_request"] = True
  live_row["start_static"] = (
    bytes((ROUTINE_SERVICE, ROUTINE_CONTROL_START)) + plan.rid.to_bytes(2, "big") + bytes(merged)
  ).hex()
  live_plan = resolve_plan(plan.ecu, live_row)
  if not isinstance(live_plan, RoutineTestPlan):
    raise ExecutorError("routine runtime materialization did not resolve a routine plan")
  if live_plan.refusals:
    raise PlanNotExecutable(live_plan)
  return live_plan


def _direct_selected_bit_mask(row: dict[str, Any], runtime_length: int) -> bytes:
  if runtime_length <= 0:
    raise ExecutorError("runtime_length must be positive")
  try:
    bit_start = registry.parse_int(row["bit_start"], "direct bit_start")
    bit_end = registry.parse_int(row["bit_end"], "direct bit_end")
  except (KeyError, registry.RegistryError) as e:
    raise ExecutorError(f"direct Active Test has no resolved control bit range: {e}") from e
  if bit_start < 0 or bit_end < bit_start or bit_end >= runtime_length * 8:
    raise ExecutorError(
      f"direct control bits {bit_start}..{bit_end} do not fit runtime length {runtime_length}")
  mask = bytearray(bit_end // 8 + 1)
  for bit in range(bit_start, bit_end + 1):
    mask[bit // 8] |= 1 << (7 - (bit & 7))
  return bytes(mask)


def _direct_type67_mask(row: dict[str, Any], runtime_length: int) -> bytes:
  try:
    bit_start = registry.parse_int(row["bit_start"], "direct bit_start")
    bit_end = registry.parse_int(row["bit_end"], "direct bit_end")
  except (KeyError, registry.RegistryError) as e:
    raise ExecutorError(f"direct Active Test has no resolved control bit range: {e}") from e
  if bit_start < 0 or bit_end < bit_start or bit_end >= runtime_length * 8:
    raise ExecutorError(
      f"direct control bits {bit_start}..{bit_end} do not fit runtime length {runtime_length}")
  records = row.get("data_id_for_act_records")
  if not isinstance(records, list) or not records:
    raise ExecutorError("mode-0 direct Active Test has no exported type-67 control-enable geometry")
  span_start = bit_start >> 3
  span_length = ((bit_end - bit_start) >> 3) + 1
  span_end = span_start + span_length
  enabled_bits: list[int] = []
  for record in records:
    if not isinstance(record, dict):
      raise ExecutorError("malformed type-67 control-enable geometry")
    try:
      control_bit_1based = registry.parse_int(record["control_enable_bit_1based"], "type67 control-enable bit")
      data_offset = registry.parse_int(record["data_byte_offset"], "type67 data byte offset")
      data_length = registry.parse_int(record["data_byte_length"], "type67 data byte length")
      mode = registry.parse_int(record["encoding_mode"], "type67 encoding mode")
    except (KeyError, registry.RegistryError) as e:
      raise ExecutorError(f"malformed type-67 control-enable geometry: {e}") from e
    if mode != 0:
      raise ExecutorError(f"mode-0 direct control references type-67 encoding mode {mode}")
    if span_start <= data_offset < span_end and data_offset + data_length <= span_end:
      if control_bit_1based <= 0:
        raise ExecutorError("type-67 control-enable bit is not 1-based positive")
      enabled_bits.append(control_bit_1based - 1)
  if not enabled_bits:
    raise ExecutorError("mode-0 direct control selected no type-67 control-enable bits")
  mask = bytearray(max(enabled_bits) // 8 + 1)
  for bit in enabled_bits:
    mask[bit // 8] |= 1 << (7 - (bit & 7))
  return bytes(mask)


def _trunc_div_toward_zero(numerator: int, denominator: int) -> int:
  if denominator == 0:
    raise ExecutorError("Toyota physical conversion divisor is zero")
  quotient = abs(numerator) // abs(denominator)
  return -quotient if (numerator < 0) != (denominator < 0) else quotient


def _direct_physical(row: dict[str, Any]) -> dict[str, Any]:
  info = row.get("signal_info")
  physical = info.get("physical") if isinstance(info, dict) else None
  if not isinstance(physical, dict):
    raise ExecutorError("direct Active Test has no exported role-0x70 engineering metadata")
  return physical


def _direct_engineering_integer_to_raw(row: dict[str, Any], engineering_integer: int) -> int:
  physical = _direct_physical(row)
  try:
    mul = registry.parse_int(physical["mul"], "direct physical mul")
    div = registry.parse_int(physical["div"], "direct physical div")
    offset = registry.parse_int(physical["offset"], "direct physical offset")
  except (KeyError, registry.RegistryError) as e:
    raise ExecutorError(f"direct Active Test has malformed engineering metadata: {e}") from e
  if mul == 0:
    raise ExecutorError("direct Active Test physical Mul is zero")
  return _trunc_div_toward_zero((engineering_integer - offset) * div, mul) & 0xFF


def direct_engineering_to_raw(row: dict[str, Any], engineering_value: str) -> int:
  """Apply current CStartActTstSnd::SetValue inverse conversion to one display value."""
  physical = _direct_physical(row)
  try:
    decimal_point_count = registry.parse_int(
      physical["decimal_point_count"], "direct physical decimal_point_count")
  except (KeyError, registry.RegistryError) as e:
    raise ExecutorError(f"direct Active Test has malformed decimal metadata: {e}") from e
  if decimal_point_count < 0:
    raise ExecutorError(f"invalid direct decimal_point_count {decimal_point_count}")
  try:
    value = Decimal(str(engineering_value))
  except InvalidOperation as e:
    raise ExecutorError(f"invalid engineering value {engineering_value!r}") from e
  if not value.is_finite():
    raise ExecutorError(f"engineering value must be finite, got {engineering_value!r}")
  scaled = value * (Decimal(10) ** decimal_point_count)
  integral = scaled.to_integral_value()
  if scaled != integral:
    raise ExecutorError(
      f"engineering value {engineering_value!r} exceeds recovered {decimal_point_count}-decimal precision")
  return _direct_engineering_integer_to_raw(row, int(integral))


def direct_choice_to_raw(row: dict[str, Any], choice: str) -> int:
  """Resolve one exact OEM role-0x70 display choice and apply SetValue conversion."""
  info = row.get("signal_info")
  choices = info.get("choices") if isinstance(info, dict) else None
  if not isinstance(choices, list):
    raise ExecutorError("direct Active Test has no exported OEM choices")
  needle = choice.casefold()
  matches = [entry for entry in choices if isinstance(entry, dict) and str(entry.get("text") or "").casefold() == needle]
  if len(matches) != 1:
    options = ", ".join(str(entry.get("text")) for entry in choices if isinstance(entry, dict) and entry.get("text"))
    if not matches:
      raise ExecutorError(f"unknown direct Active Test choice {choice!r}; available: {options or '(none)'}")
    raise ExecutorError(f"ambiguous direct Active Test choice {choice!r}")
  try:
    engineering_integer = registry.parse_int(matches[0]["value"], "direct choice value")
  except (KeyError, registry.RegistryError) as e:
    raise ExecutorError(f"malformed direct choice metadata: {e}") from e
  return _direct_engineering_integer_to_raw(row, engineering_integer)


def pack_direct_raw_value(row: dict[str, Any], runtime_length: int, raw_value: int) -> bytes:
  """Pack one raw scalar exactly like current GTS+ P5 direct Active-Test modes 0/1/3/4."""
  if runtime_length <= 0:
    raise ExecutorError("runtime_length must be positive")
  try:
    encoding_mode = registry.parse_int(row["encoding_mode"], "direct encoding_mode")
    bit_start = registry.parse_int(row["bit_start"], "direct bit_start")
    bit_end = registry.parse_int(row["bit_end"], "direct bit_end")
  except (KeyError, registry.RegistryError) as e:
    raise ExecutorError(f"direct Active Test has incomplete scalar packing metadata: {e}") from e
  if bit_start < 0 or bit_end < bit_start or bit_end >= runtime_length * 8:
    raise ExecutorError(
      f"direct value bits {bit_start}..{bit_end} do not fit runtime length {runtime_length}")
  width = bit_end - bit_start + 1
  if raw_value < 0 or raw_value >= (1 << width):
    raise ExecutorError(
      f"raw direct value {raw_value} does not fit recovered {width}-bit field {bit_start}..{bit_end}")

  payload = bytearray(runtime_length)
  start_byte = bit_start >> 3
  end_byte = bit_end >> 3

  if encoding_mode in {0, 3}:
    if (bit_start & 7) != 0 or (bit_end & 7) != 7:
      raise ExecutorError(
        f"direct encoding mode {encoding_mode} requires byte-aligned field, got {bit_start}..{bit_end}")
    byte_width = end_byte - start_byte + 1
    if not 1 <= byte_width <= 4:
      raise ExecutorError(f"direct encoding mode {encoding_mode} field width {byte_width} byte(s) is unsupported")
    payload[start_byte:end_byte + 1] = raw_value.to_bytes(byte_width, "big")
    return bytes(payload)

  if encoding_mode == 1:
    if start_byte != end_byte or width > 8:
      raise ExecutorError(f"direct encoding mode 1 requires a <=8-bit field within one byte, got {bit_start}..{bit_end}")
    shift = 7 - (bit_end & 7)
    payload[end_byte] = (raw_value << shift) & 0xFF
    return bytes(payload)

  if encoding_mode == 4:
    byte_width = end_byte - start_byte + 1
    if not 1 <= byte_width <= 4:
      raise ExecutorError(f"direct encoding mode 4 field width {byte_width} byte(s) is unsupported")
    shift = 7 - (bit_end & 7)
    shifted = raw_value << shift
    if shifted >= (1 << (byte_width * 8)):
      raise ExecutorError(
        f"shifted direct value 0x{shifted:X} does not fit selected {byte_width}-byte span")
    payload[start_byte:end_byte + 1] = shifted.to_bytes(byte_width, "big")
    return bytes(payload)

  raise ExecutorError(f"direct encoding mode {encoding_mode} has no recovered scalar packer")


def direct_control_enable_masks(row: dict[str, Any], runtime_length: int) -> tuple[bytes, bytes]:
  """Materialize current GTS+ start/stop control-enable masks for a direct P5 test."""
  try:
    encoding_mode = registry.parse_int(row["encoding_mode"], "direct encoding_mode")
  except (KeyError, registry.RegistryError) as e:
    raise ExecutorError(f"direct Active Test has no resolved encoding mode: {e}") from e
  strategy = row.get("control_enable_mask")
  if not isinstance(strategy, dict):
    raise ExecutorError("direct Active Test has no exported control-enable-mask strategy")
  expected = {
    0: ("type67_rows_for_selected_byte_span", "type67_rows_for_selected_byte_span"),
    1: ("none", "selected_bit_range"),
    3: ("none", "none"),
    4: ("none", "selected_bit_range"),
  }.get(encoding_mode)
  if expected is None:
    raise ExecutorError(f"direct encoding mode {encoding_mode} has no recovered mask materializer")
  actual = (strategy.get("start"), strategy.get("stop"))
  if actual != expected:
    raise ExecutorError(f"direct mask strategy {actual!r} does not match recovered mode-{encoding_mode} strategy {expected!r}")
  if encoding_mode == 0:
    mask = _direct_type67_mask(row, runtime_length)
    return mask, mask
  if encoding_mode in {1, 4}:
    return b"", _direct_selected_bit_mask(row, runtime_length)
  return b"", b""


def direct_control_enable_mask(row: dict[str, Any], runtime_length: int) -> bytes:
  """Compatibility helper returning the recovered return-control mask only."""
  return direct_control_enable_masks(row, runtime_length)[1]


def runtime_refusals(profile: registry.Profile, plan: TestPlan) -> tuple[str, ...]:
  """Execution gates beyond static wire geometry; pure and zero-transmit."""
  refusals = list(plan.refusals)
  if plan.session_requirement == SESSION_REQUIREMENT_EXTENDED:
    try:
      lifecycle = parse_lifecycle(profile, plan.ecu)
    except (registry.RegistryError, LifecycleError) as e:
      refusals.append(f"recovered lifecycle metadata is not executable: {e}")
    else:
      if lifecycle is None:
        refusals.append("registry supplies no recovered session lifecycle")
  return tuple(refusals)


def _resolve_direct(ecu: EcuSpec, test_id: int, name: str, row: dict[str, Any]) -> DirectTestPlan:
  refusals = _base_refusals(row, "direct", DIRECT_SERVICE, DIRECT_POSITIVE_SID)
  kwargs: dict[str, Any] = {}
  try:
    did = registry.parse_int(row["did"], "direct did")
    if did == 0xFFFF:
      refusals.append("direct DID is unresolved placeholder 0xFFFF")
    start_prefix = registry.parse_bytes(row["start_prefix"], "direct start_prefix")
    stop_prefix = registry.parse_bytes(row["stop_prefix"], "direct stop_prefix")
    start = _decompose_direct_prefix(start_prefix, did, "start_prefix")
    stop = _decompose_direct_prefix(stop_prefix, did, "stop_prefix")
    if isinstance(start, str) or isinstance(stop, str):
      raise registry.RegistryError(start if isinstance(start, str) else stop)
    kwargs.update(did=did, start_control=start[0], start_option_prefix=start[1],
                  stop_control=stop[0], stop_option_prefix=stop[1])
  except (KeyError, registry.RegistryError) as e:
    refusals.append(f"malformed direct plan: {e}")
    return DirectTestPlan(ecu=ecu, test_id=test_id, name=name, kind="direct",
                          refusals=tuple(refusals))
  runtime_length = row.get("runtime_length")
  if runtime_length is None:
    minimum = row.get("runtime_length_minimum")
    known = f" (only a bit-geometry minimum of {minimum} is known)" if minimum is not None else ""
    refusals.append(f"runtime payload length not definitively recovered{known}")
  else:
    try:
      length = registry.parse_int(runtime_length, "direct runtime_length")
      if length <= 0:
        raise registry.RegistryError(f"direct runtime_length must be positive: {length}")
      kwargs["runtime_length"] = length
    except registry.RegistryError as e:
      refusals.append(f"malformed direct plan: {e}")
  return DirectTestPlan(ecu=ecu, test_id=test_id, name=name, kind="direct",
                        session_requirement=row.get("session_requirement"),
                        positive_sid=DIRECT_POSITIVE_SID, refusals=tuple(refusals), **kwargs)


def _resolve_routine(ecu: EcuSpec, test_id: int, name: str, row: dict[str, Any]) -> RoutineTestPlan:
  refusals = _base_refusals(row, "routine", ROUTINE_SERVICE, ROUTINE_POSITIVE_SID)
  kwargs: dict[str, Any] = {}
  try:
    rid = registry.parse_int(row["routine_id"], "routine routine_id")
    if rid == 0xFFFF:
      refusals.append("routine identifier is unresolved placeholder 0xFFFF")
    start_option = _decompose_routine_static(registry.parse_bytes(row["start_static"], "routine start_static"),
                                             ROUTINE_CONTROL_START, rid, "start_static")
    stop_option = _decompose_routine_static(registry.parse_bytes(row["stop_static"], "routine stop_static"),
                                            ROUTINE_CONTROL_STOP, rid, "stop_static")
    if isinstance(start_option, str) or isinstance(stop_option, str):
      raise registry.RegistryError(start_option if isinstance(start_option, str) else stop_option)
    kwargs.update(rid=rid, start_option_prefix=start_option, stop_option_prefix=stop_option)
    result_static = row.get("result_static")
    if result_static is not None:
      status_option = _decompose_routine_static(registry.parse_bytes(result_static, "routine result_static"),
                                                ROUTINE_CONTROL_RESULTS, rid, "result_static")
      if isinstance(status_option, str):
        raise registry.RegistryError(status_option)
      kwargs.update(status_control=ROUTINE_CONTROL_RESULTS, status_option_prefix=status_option)
  except (KeyError, registry.RegistryError) as e:
    refusals.append(f"malformed routine plan: {e}")
  return RoutineTestPlan(ecu=ecu, test_id=test_id, name=name, kind="routine",
                         session_requirement=row.get("session_requirement"),
                         positive_sid=ROUTINE_POSITIVE_SID, parameterized=not bool(row.get("fixed_request")),
                         refusals=tuple(refusals), **kwargs)


# -- hold/keepalive loop -------------------------------------------------------------------------------------------
def _hold(session: DiagnosticSession, *, hold_s: float, status_fn: Callable[[], bytes] | None,
          poll_interval_s: float | None, sleep: Callable[[float], None],
          clock: Callable[[], float]) -> tuple[tuple[float, bytes], ...]:
  """Hold an operation: run recovered keepalive and optional status polls until the deadline."""
  lifecycle = session.lifecycle
  keepalive = lifecycle.keepalive if lifecycle is not None else None
  if keepalive is None and status_fn is None:
    sleep(hold_s)
    return ()
  poll_interval = poll_interval_s if poll_interval_s is not None else 0.0
  now = clock()
  deadline = now + hold_s
  next_poll = now + poll_interval if status_fn is not None else float("inf")
  next_keepalive = now + keepalive.interval_s if keepalive is not None else float("inf")
  samples: list[tuple[float, bytes]] = []
  while True:
    now = clock()
    if now >= deadline:
      return tuple(samples)
    if status_fn is not None and now >= next_poll:
      samples.append((now, status_fn()))
      next_poll = now + poll_interval
      continue
    if keepalive is not None and now >= next_keepalive:
      session.keepalive()
      next_keepalive = now + keepalive.interval_s
      continue
    step = deadline - now
    if status_fn is not None:
      step = min(step, max(next_poll - now, 0.0))
    if keepalive is not None:
      step = min(step, max(next_keepalive - now, 0.0))
    sleep(step)


# -- run backends ----------------------------------------------------------------------------------------------------
def _prepare(session: DiagnosticSession, plan: TestPlan, *, execute: bool) -> str | None:
  """Enter the recovered Toyota lifecycle when an acknowledged operation requires it.

  Without an explicit execute acknowledgement this performs zero transmissions and
  the caller gets a plan-only result instead.
  """
  if not execute:
    return plan.session_requirement
  refusals = runtime_refusals(session.profile, plan)
  if refusals:
    raise PlanNotExecutable(plan, refusals)
  if plan.session_requirement == SESSION_REQUIREMENT_EXTENDED:
    session.enter_extended()
  return plan.session_requirement


def _routine_stop(client, plan: RoutineTestPlan) -> bytes:
  return client.routine_control(ROUTINE_CONTROL_TYPE(plan.stop_control), plan.rid, plan.stop_option_prefix)


def run_direct_test(session: DiagnosticSession, plan: DirectTestPlan, *, hold_s: float,
                    value_payload: bytes = b"", start_control_enable_mask: bytes = b"",
                    stop_control_enable_mask: bytes = b"", execute: bool = False,
                    echo: Callable[[str], None] = print, sleep: Callable[[float], None] = time.sleep,
                    clock: Callable[[], float] = time.monotonic) -> ActiveTestResult:
  """Run a recovered 0x2F Active Test: start -> hold (keepalive) -> stop/return control.

  `value_payload` is the explicit caller-supplied N-byte option record. Start and
  stop control-enable masks are separately materialized from Toyota's recovered
  encoding-mode geometry and may be shorter than N (or empty).
  """
  if plan.kind != "direct" or not isinstance(plan, DirectTestPlan):
    raise ExecutorError(f"expected a direct plan, got kind {plan.kind!r}")
  if hold_s <= 0:
    raise ExecutorError("hold_s must be positive")
  if execute:
    if plan.runtime_length is None:
      raise PlanNotExecutable(plan)
    if len(value_payload) != plan.runtime_length:
      raise ExecutorError(f"value_payload must be exactly {plan.runtime_length} byte(s), got {len(value_payload)}")

  session_requirement = _prepare(session, plan, execute=execute)
  if not execute:
    return ActiveTestResult(plan=plan, executed=False, session_requirement=session_requirement)

  client = session.client()

  def start_control() -> bytes:
    return client.input_output_control_by_identifier(
      plan.did, CONTROL_PARAMETER_TYPE(plan.start_control), plan.start_option_prefix + value_payload,
      start_control_enable_mask)

  def stop_control() -> bytes:
    return client.input_output_control_by_identifier(
      plan.did, CONTROL_PARAMETER_TYPE(plan.stop_control), plan.stop_option_prefix, stop_control_enable_mask)

  started = False
  cleanup_errors: list[str] = []
  try:
    start = start_control()
    started = True
    statuses = _hold(session, hold_s=hold_s, status_fn=None, poll_interval_s=None, sleep=sleep, clock=clock)
    stop = stop_control()
  except BaseException as e:
    if started:
      _best_effort_stop(cleanup_errors, stop_control)
    _attach_cleanup_errors(e, cleanup_errors)
    raise
  return ActiveTestResult(plan=plan, executed=True, session_requirement=session_requirement, start=start,
                          statuses=statuses, stop=stop, cleanup_errors=tuple(cleanup_errors + session.cleanup_errors))


def run_multi_direct_test(
    session: DiagnosticSession,
    plan: MultiDirectTestPlan,
    *,
    hold_s: float,
    raw_values: dict[int, int],
    execute: bool = False,
    echo: Callable[[str], None] = print,
    sleep: Callable[[float], None] = time.sleep,
    clock: Callable[[], float] = time.monotonic,
) -> ActiveTestResult:
  """Run one recovered type-33 current-P5 group as a single OR-composed 0x2F control."""
  if plan.kind != "multi_direct" or not isinstance(plan, MultiDirectTestPlan):
    raise ExecutorError(f"expected a multi-direct plan, got kind {plan.kind!r}")
  if hold_s <= 0:
    raise ExecutorError("hold_s must be positive")
  if execute and (plan.runtime_length is None or plan.refusals):
    raise PlanNotExecutable(plan)
  session_requirement = _prepare(session, plan, execute=execute)
  if not execute:
    return ActiveTestResult(plan=plan, executed=False, session_requirement=session_requirement)

  payload, start_mask, stop_mask = compose_multi_direct_payload(plan, raw_values)
  client = session.client()

  def start_control() -> bytes:
    return client.input_output_control_by_identifier(
      plan.did, CONTROL_PARAMETER_TYPE.SHORT_TERM_ADJUSTMENT, payload, start_mask)

  def stop_control() -> bytes:
    return client.input_output_control_by_identifier(
      plan.did, CONTROL_PARAMETER_TYPE.RETURN_CONTROL_TO_ECU, b"", stop_mask)

  started = False
  cleanup_errors: list[str] = []
  try:
    start = start_control()
    started = True
    statuses = _hold(session, hold_s=hold_s, status_fn=None, poll_interval_s=None, sleep=sleep, clock=clock)
    stop = stop_control()
  except BaseException as e:
    if started:
      _best_effort_stop(cleanup_errors, stop_control)
    _attach_cleanup_errors(e, cleanup_errors)
    raise
  return ActiveTestResult(
    plan=plan, executed=True, session_requirement=session_requirement, start=start,
    statuses=statuses, stop=stop, cleanup_errors=tuple(cleanup_errors + session.cleanup_errors),
  )


def stop_multi_direct_test(
    session: DiagnosticSession,
    plan: MultiDirectTestPlan,
    *,
    execute: bool = False,
) -> ActiveTestResult:
  """Send the recovered OR-composed return-control mask for one current P5 type-33 group."""
  session_requirement = _prepare(session, plan, execute=execute)
  if not execute:
    return ActiveTestResult(plan=plan, executed=False, session_requirement=session_requirement)
  if plan.runtime_length is None or plan.refusals:
    raise PlanNotExecutable(plan)
  _, _, stop_mask = compose_multi_direct_payload(plan, {member_id: 0 for member_id in plan.member_ids})
  stop = session.client().input_output_control_by_identifier(
    plan.did, CONTROL_PARAMETER_TYPE.RETURN_CONTROL_TO_ECU, b"", stop_mask)
  return ActiveTestResult(
    plan=plan, executed=True, session_requirement=session_requirement, stop=stop,
    cleanup_errors=tuple(session.cleanup_errors),
  )


def run_routine_test(session: DiagnosticSession, plan: RoutineTestPlan, *, hold_s: float,
                     option_record: bytes | None = None, execute: bool = False, poll_interval_s: float = 0.5,
                     echo: Callable[[str], None] = print, sleep: Callable[[float], None] = time.sleep,
                     clock: Callable[[], float] = time.monotonic) -> ActiveTestResult:
  """Run a recovered 0x31 Active Test/utility: start -> status polls/keepalive -> stop.

  Parameterized routines (registry `fixed_request` false) require an explicit
  `option_record`; fixed routines refuse one — static bytes alone never authorize
  invented runtime data.
  """
  if plan.kind != "routine" or not isinstance(plan, RoutineTestPlan):
    raise ExecutorError(f"expected a routine plan, got kind {plan.kind!r}")
  if hold_s <= 0:
    raise ExecutorError("hold_s must be positive")
  if not execute:
    session_requirement = _prepare(session, plan, execute=False)
    return ActiveTestResult(plan=plan, executed=False, session_requirement=session_requirement)

  refusals = runtime_refusals(session.profile, plan)
  if refusals:
    raise PlanNotExecutable(plan, refusals)
  if plan.parameterized:
    if not option_record:
      raise ExecutorError("registry marks this routine parameterized; explicit option_record bytes are required")
  elif option_record:
    raise ExecutorError("fixed routine takes no runtime option record")

  session_requirement = _prepare(session, plan, execute=True)
  client = session.client()
  start_option = plan.start_option_prefix + (option_record if plan.parameterized else b"")

  def start_control() -> bytes:
    return client.routine_control(ROUTINE_CONTROL_TYPE(plan.start_control), plan.rid, start_option)

  def emergency_stop() -> None:
    _routine_stop(client, plan)

  started = False
  cleanup_errors: list[str] = []
  try:
    start = start_control()
    started = True
    statuses = _hold(session, hold_s=hold_s, status_fn=_status_poller(client, plan),
                     poll_interval_s=poll_interval_s, sleep=sleep, clock=clock)
    stop = _routine_stop(client, plan)
  except BaseException as e:
    if started:
      _best_effort_stop(cleanup_errors, emergency_stop)
    _attach_cleanup_errors(e, cleanup_errors)
    raise
  return ActiveTestResult(plan=plan, executed=True, session_requirement=session_requirement, start=start,
                          statuses=statuses, stop=stop, cleanup_errors=tuple(cleanup_errors + session.cleanup_errors))


def stop_test(session: DiagnosticSession, plan: TestPlan, *, control_enable_mask: bytes = b"",
              execute: bool = False, echo: Callable[[str], None] = print) -> ActiveTestResult:
  """Explicit recovery/stop surface for an already-running recovered Active Test.

  Routine stops use the recovered fixed 0x31 stop request. Direct controls use
  the recovered mode-specific return-control mask supplied by the caller. Like
  normal execution, no transmission occurs without `execute=True`.
  """
  session_requirement = _prepare(session, plan, execute=execute)
  if not execute:
    return ActiveTestResult(plan=plan, executed=False, session_requirement=session_requirement)

  client = session.client()
  if isinstance(plan, RoutineTestPlan):
    stop = _routine_stop(client, plan)
  elif isinstance(plan, DirectTestPlan):
    if plan.runtime_length is None:
      raise PlanNotExecutable(plan)
    stop = client.input_output_control_by_identifier(
      plan.did, CONTROL_PARAMETER_TYPE(plan.stop_control), plan.stop_option_prefix, stop_control_enable_mask)
  else:
    raise ExecutorError(f"expected a routine or direct plan, got kind {plan.kind!r}")
  return ActiveTestResult(plan=plan, executed=True, session_requirement=session_requirement, stop=stop,
                          cleanup_errors=tuple(session.cleanup_errors))


def _status_poller(client, plan: RoutineTestPlan) -> Callable[[], bytes] | None:
  if plan.status_control is None:
    return None

  def poll() -> bytes:
    return client.routine_control(ROUTINE_CONTROL_TYPE(plan.status_control), plan.rid, plan.status_option_prefix)

  return poll


def _attach_cleanup_errors(error: BaseException, errors: list[str]) -> None:
  if not errors:
    return
  try:
    error.toyota_cleanup_errors = tuple(errors)
  except BaseException:
    pass


def _best_effort_stop(errors: list[str], stop: Callable[[], Any]) -> None:
  try:
    stop()
  except BaseException as e:  # guarantee the original failure propagates
    errors.append(f"emergency stop failed: {e!r}")
