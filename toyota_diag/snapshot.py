"""Toyota all-system read-only Health Check foundation.

This surface starts from Toyota's resolved vehicle/install-set model rather than
the maintainer Camry's historical address sweep. It first executes only the
family-local mount/support probes the runtime has recovered, then collects DTCs,
exact exported generic-CID identity reads, and raw generic P5 per-DTC freeze-frame
records from responding logical ECUs where those exact command contracts exist.

GTS+ Health Check also stores Info Code, Operation History, monitor, and optional
timestamp data. Those surfaces remain explicit coverage gaps in the snapshot
instead of being silently approximated here.
"""
from __future__ import annotations

from datetime import datetime, timezone
import json
from pathlib import Path
from typing import Any

from opendbc.car.uds import MessageTimeoutError, NegativeResponseError

from toyota_diag import dtc, registry, resolver, transport
from toyota_diag.executor import SESSION_REQUIREMENT_EXTENDED
from toyota_diag.session import DiagnosticSession

SCHEMA = "toyota-health-check-v1"


def _endpoint_key(address: int, sub_addr: int | None) -> tuple[int, int | None]:
  return int(address), None if sub_addr is None else int(sub_addr)


def _mount_by_endpoint(profile: registry.Profile, rows: list[dict[str, Any]]) -> dict[tuple[int, int | None], dict[str, Any]]:
  out: dict[tuple[int, int | None], dict[str, Any]] = {}
  for row in rows:
    route = row.get("transport_route")
    if not isinstance(route, dict):
      continue
    try:
      parsed = resolver.route_for_candidate(row)
    except resolver.ResolverError:
      continue
    out[_endpoint_key(parsed.request_address, parsed.sub_addr)] = row
  return out


def _catalog_command(profile: registry.Profile, ecu: registry.EcuSpec, kind: str) -> dict[str, Any] | None:
  catalog = profile.category(ecu)
  if not isinstance(catalog, dict):
    return None
  rows = [row for row in catalog.get("commands", []) if isinstance(row, dict) and row.get("kind") == kind]
  return rows[0] if len(rows) == 1 else None


def _generic_cid_plan(profile: registry.Profile, ecu: registry.EcuSpec) -> tuple[dict[str, Any], int] | None:
  """Return the exact exported generic-CID command and DID, or None."""
  command = _catalog_command(profile, ecu, "generic_cid")
  if command is None:
    return None
  requests = [row for row in command.get("requests", []) if isinstance(row, dict) and row.get("resolved")]
  if len(requests) != 1:
    return None
  try:
    request = registry.parse_bytes(requests[0].get("send"), "generic_cid request")
    check = registry.parse_bytes(requests[0].get("check"), "generic_cid positive check")
  except registry.RegistryError:
    return None
  if len(request) != 3 or request[0] != 0x22:
    return None
  did = int.from_bytes(request[1:], "big")
  if len(check) < 3 or check[:3] != bytes([0x62]) + request[1:]:
    return None
  return command, did


def _printable_ascii(data: bytes) -> str | None:
  if not data:
    return None
  text = data.rstrip(b"\x00").decode("ascii", errors="ignore").strip()
  if not text or not all(32 <= ord(char) < 127 for char in text):
    return None
  return text


def _p5_snapshot_plan(profile: registry.Profile, ecu: registry.EcuSpec) -> dict[str, Any] | None:
  """Return an exact exported ordinary-P5 per-DTC snapshot command, or None."""
  command = _catalog_command(profile, ecu, "p5_dtc_snapshot")
  if command is None or command.get("execution") != "read_only":
    return None
  requests = [row for row in command.get("requests", []) if isinstance(row, dict) and row.get("resolved")]
  if len(requests) != 1:
    return None
  try:
    request = registry.parse_bytes(requests[0].get("send"), "p5_dtc_snapshot request")
    check = registry.parse_bytes(requests[0].get("check"), "p5_dtc_snapshot positive check")
  except registry.RegistryError:
    return None
  if request != bytes.fromhex("1904000000ff") or check != bytes.fromhex("5904"):
    return None
  binding = command.get("plugin_binding")
  if not isinstance(binding, dict) or binding.get("dll") != "GetEachFrzFrmDatP5_DT.dll":
    return None
  return command


def _rob_inventory_plan(profile: registry.Profile, ecu: registry.EcuSpec) -> dict[str, Any] | None:
  """Return the exact exported ordinary-P5 RoB behavior-code inventory command."""
  command = _catalog_command(profile, ecu, "p5_rob_code_inventory")
  if command is None or command.get("execution") != "read_only":
    return None
  binding = command.get("plugin_binding")
  if not isinstance(binding, dict) or binding.get("dll") != "GetRoBP5_DT.dll" or binding.get("exact_category_binding") is not True:
    return None
  requests = [row for row in command.get("requests", []) if isinstance(row, dict) and row.get("resolved")]
  if len(requests) != 2:
    return None
  try:
    shape = sorted((registry.parse_bytes(row.get("send"), "RoB request"),
                    registry.parse_bytes(row.get("check"), "RoB positive check")) for row in requests)
  except registry.RegistryError:
    return None
  if shape != [(bytes.fromhex("ab01"), bytes.fromhex("eb01")), (bytes.fromhex("ab11"), bytes.fromhex("eb11"))]:
    return None
  return command


def _rob_inventory_from_client(command: dict[str, Any] | None, client) -> dict[str, Any]:
  if command is None:
    return {"state": "not_available", "groups": [], "behavior_code_count": 0, "unique_behavior_code_count": 0}
  groups: list[dict[str, Any]] = []
  all_codes: list[int] = []
  for row in command.get("requests", []):
    try:
      request = registry.parse_bytes(row.get("send"), "RoB request")
      check = registry.parse_bytes(row.get("check"), "RoB positive check")
    except registry.RegistryError as e:
      groups.append({"state": "error", "error": str(e), "behavior_codes": []})
      continue
    try:
      response = bytes(transport.raw_isotp(client, request))
    except MessageTimeoutError:
      groups.append({"subfunction": request[1], "state": "no_response", "behavior_codes": []})
      continue
    except Exception as e:
      groups.append({"subfunction": request[1], "state": "error", "behavior_codes": [], "error": str(e)})
      continue
    if len(response) >= 3 and response[0] == 0x7F:
      groups.append({
        "subfunction": request[1], "state": "negative_response", "behavior_codes": [],
        "response_hex": response.hex(), "nrc": response[2],
      })
      continue
    if not response.startswith(check):
      groups.append({
        "subfunction": request[1], "state": "parse_error", "behavior_codes": [],
        "response_hex": response.hex(), "error": f"expected response prefix {check.hex()}",
      })
      continue
    payload = response[len(check):]
    if len(payload) % 2:
      groups.append({
        "subfunction": request[1], "state": "parse_error", "behavior_codes": [],
        "response_hex": response.hex(), "error": f"odd behavior-code payload length {len(payload)}",
      })
      continue
    codes = [int.from_bytes(payload[index:index + 2], "big") for index in range(0, len(payload), 2)]
    all_codes.extend(codes)
    groups.append({
      "subfunction": request[1], "state": "positive", "behavior_codes": codes,
      "response_hex": response.hex(),
    })
  return {
    "state": "available",
    "groups": groups,
    "behavior_code_count": len(all_codes),
    "unique_behavior_code_count": len(set(all_codes)),
    "record_bodies": "not_retrieved",
  }


def _identity_from_client(plan: tuple[dict[str, Any], int] | None, client) -> dict[str, Any] | None:
  if plan is None:
    return None
  _, did = plan
  try:
    value = bytes(client.read_data_by_identifier(did))
  except MessageTimeoutError:
    return {"did": did, "state": "no_response", "data_hex": None, "ascii": None}
  except NegativeResponseError as e:
    return {"did": did, "state": "negative_response", "data_hex": None, "ascii": None, "error": str(e)}
  except Exception as e:
    return {"did": did, "state": "error", "data_hex": None, "ascii": None, "error": str(e)}
  return {"did": did, "state": "positive", "data_hex": value.hex(), "ascii": _printable_ascii(value)}


def _freeze_frames_from_client(command: dict[str, Any] | None, client, dtc_rows: list[dict[str, Any]]) -> dict[str, Any]:
  if command is None:
    return {"state": "not_available", "dtcs": [], "positive_dtc_count": 0, "record_count": 0}
  results: list[dict[str, Any]] = []
  positive = 0
  record_count = 0
  for row in dtc_rows:
    code = str(row["code"])
    try:
      parsed = dtc.read_p5_dtc_snapshots(client, code)
    except MessageTimeoutError:
      results.append({"dtc": code, "state": "no_response", "records": []})
      continue
    except NegativeResponseError as e:
      results.append({"dtc": code, "state": "negative_response", "records": [], "error": str(e)})
      continue
    except (ValueError, TypeError) as e:
      results.append({"dtc": code, "state": "parse_error", "records": [], "error": str(e)})
      continue
    except Exception as e:
      results.append({"dtc": code, "state": "error", "records": [], "error": str(e)})
      continue
    records = list(parsed["records"])
    positive += 1
    record_count += len(records)
    results.append({
      "dtc": code,
      "state": "positive",
      "status": int(parsed["status"]),
      "records": records,
    })
  return {
    "state": "available",
    "dtcs": results,
    "positive_dtc_count": positive,
    "record_count": record_count,
    "signal_decode": "raw_only",
  }


def _extended_details(
    profile: registry.Profile, ecu: registry.EcuSpec, client_factory,
    dtc_result: dict[str, Any], *, include_identities: bool,
) -> tuple[dict[str, Any] | None, dict[str, Any], dict[str, Any]]:
  identity_plan = _generic_cid_plan(profile, ecu) if include_identities else None
  ffd_plan = _p5_snapshot_plan(profile, ecu)
  rob_plan = _rob_inventory_plan(profile, ecu)
  dtc_rows = list(dtc_result.get("records") or []) if dtc_result.get("state") == "positive" else []
  identity = None
  if ffd_plan is None:
    freeze_frames = {"state": "not_available", "dtcs": [], "positive_dtc_count": 0, "record_count": 0}
  elif dtc_result.get("state") != "positive":
    freeze_frames = {"state": "not_queried", "dtcs": [], "positive_dtc_count": 0, "record_count": 0}
  elif not dtc_rows:
    freeze_frames = {"state": "available", "dtcs": [], "positive_dtc_count": 0, "record_count": 0, "signal_decode": "raw_only"}
  else:
    freeze_frames = None
  rob = None if rob_plan is not None else {
    "state": "not_available", "groups": [], "behavior_code_count": 0, "unique_behavior_code_count": 0,
  }

  needs_identity = identity_plan is not None
  needs_ffd = freeze_frames is None
  needs_rob = rob is None
  if not needs_identity and not needs_ffd and not needs_rob:
    return identity, freeze_frames, rob

  operation = identity_plan[0] if identity_plan is not None else (ffd_plan if needs_ffd else rob_plan)
  session = DiagnosticSession(profile, ecu, client_factory=client_factory, operation_row=operation)
  try:
    with session:
      plans = [
        plan for plan in (
          identity_plan[0] if identity_plan else None,
          ffd_plan if needs_ffd else None,
          rob_plan if needs_rob else None,
        ) if plan is not None
      ]
      requests = [request for plan in plans for request in plan.get("requests", []) if request.get("resolved")]
      if any(request.get("session_requirement") == SESSION_REQUIREMENT_EXTENDED for request in requests):
        session.enter_extended()
      client = session.client()
      if needs_identity:
        identity = _identity_from_client(identity_plan, client)
      if needs_ffd:
        freeze_frames = _freeze_frames_from_client(ffd_plan, client, dtc_rows)
      if needs_rob:
        rob = _rob_inventory_from_client(rob_plan, client)
  except Exception as e:
    message = str(e)
    if needs_identity and identity is None:
      _, did = identity_plan
      identity = {"did": did, "state": "error", "data_hex": None, "ascii": None, "error": message}
    if needs_ffd and freeze_frames is None:
      freeze_frames = {"state": "error", "dtcs": [], "positive_dtc_count": 0, "record_count": 0, "error": message}
    if needs_rob and rob is None:
      rob = {"state": "error", "groups": [], "behavior_code_count": 0, "unique_behavior_code_count": 0, "error": message}
  assert freeze_frames is not None
  assert rob is not None
  if session.cleanup_errors:
    cleanup = list(session.cleanup_errors)
    freeze_frames["cleanup_errors"] = cleanup
    rob["cleanup_errors"] = cleanup
    if identity is not None:
      identity["cleanup_errors"] = cleanup
  return identity, freeze_frames, rob


def _dtcs(profile: registry.Profile, ecu: registry.EcuSpec, client_factory) -> dict[str, Any]:
  try:
    data = client_factory(ecu.address, ecu.sub_addr).read_dtc_information(
      dtc.DTC_REPORT_TYPE.DTC_BY_STATUS_MASK, dtc.DTC_STATUS_MASK_TYPE.ALL)
    records = dtc.parse_dtc_response(data)
  except MessageTimeoutError:
    return {"state": "no_response", "records": [], "fault_count": 0}
  except NegativeResponseError as e:
    return {"state": "negative_response", "records": [], "fault_count": 0, "error": str(e)}
  except Exception as e:
    return {"state": "error", "records": [], "fault_count": 0, "error": str(e)}

  rows = []
  for code, status in records:
    rows.append({
      "code": code,
      "status": status,
      "status_bits": registry.decode_status_bits(status),
      "fault_status": bool(status & profile.fault_status_mask),
      "descriptions": profile.describe_dtc(ecu, code),
    })
  return {"state": "positive", "records": rows, "fault_count": sum(row["fault_status"] for row in rows)}


def build(profile: registry.Profile, client_factory, transport_state: dict[str, Any] | None = None, *,
          include_identities: bool = True) -> dict[str, Any]:
  """Collect one all-system read-only Health Check from Toyota's vehicle profile."""
  if profile.vehicle_type is None or profile.vehicle_resolution is None:
    raise registry.RegistryError("Health Check requires a selected Toyota vehicle profile")

  mount_rows = resolver.probe_mount_candidates(profile, client_factory)
  mount_lookup = _mount_by_endpoint(profile, mount_rows)
  ecus: list[dict[str, Any]] = []
  fault_count = 0
  dtc_positive = 0
  identity_positive = 0
  freeze_frame_positive_dtcs = 0
  freeze_frame_records = 0
  freeze_frame_available_ecus = 0
  rob_available_ecus = 0
  rob_behavior_codes = 0

  for ecu in profile.ecus:
    mount = mount_lookup.get(ecu.endpoint)
    responding = bool(mount and mount.get("transport_responded") is True)
    dtc_result = {"state": "not_queried", "records": [], "fault_count": 0}
    identity = None
    freeze_frames = {"state": "not_queried", "dtcs": [], "positive_dtc_count": 0, "record_count": 0}
    rob = {"state": "not_queried", "groups": [], "behavior_code_count": 0, "unique_behavior_code_count": 0}
    if responding:
      dtc_result = _dtcs(profile, ecu, client_factory)
      dtc_positive += int(dtc_result["state"] == "positive")
      fault_count += int(dtc_result["fault_count"])
      identity, freeze_frames, rob = _extended_details(
        profile, ecu, client_factory, dtc_result, include_identities=include_identities)
      identity_positive += int(identity is not None and identity.get("state") == "positive")
      freeze_frame_available_ecus += int(freeze_frames.get("state") == "available")
      freeze_frame_positive_dtcs += int(freeze_frames.get("positive_dtc_count", 0))
      freeze_frame_records += int(freeze_frames.get("record_count", 0))
      rob_available_ecus += int(rob.get("state") == "available")
      rob_behavior_codes += int(rob.get("behavior_code_count", 0))

    ecus.append({
      "key": ecu.key,
      "name": ecu.name,
      "category_id": ecu.category_id,
      "generation": ecu.generation,
      "address": ecu.address,
      "sub_addr": ecu.sub_addr,
      "transport_kind": ecu.transport_kind,
      "controller": ecu.controller,
      "mount": None if mount is None else {
        "install_set_id": mount.get("install_set_id"),
        "live_state": mount.get("live_state"),
        "transport_responded": mount.get("transport_responded"),
        "probe_available": mount.get("probe_available"),
        "support_mode": mount.get("support_mode"),
        "support_root": mount.get("support_root"),
        "supported_group_count": mount.get("supported_group_count"),
        "probe_error": mount.get("probe_error") or mount.get("support_error"),
      },
      "dtc": dtc_result,
      "freeze_frames": freeze_frames,
      "rob": rob,
      "identity": identity,
    })

  responding_count = sum(row.get("transport_responded") is True for row in mount_rows)
  return {
    "schema": SCHEMA,
    "captured_at": datetime.now(timezone.utc).isoformat(),
    "profile": profile.name,
    "vehicle": profile.vehicle,
    "vehicle_type": profile.vehicle_type,
    "region": profile.region,
    "logical_bus": profile.bus,
    "transport": transport_state,
    "summary": {
      "install_candidates": len(mount_rows),
      "mount_responding": responding_count,
      "mount_no_response": sum(row.get("live_state") == "no_response" for row in mount_rows),
      "mount_probe_unavailable": sum(row.get("live_state") == "probe_unavailable" for row in mount_rows),
      "dtc_positive_ecus": dtc_positive,
      "identity_positive_ecus": identity_positive,
      "freeze_frame_available_ecus": freeze_frame_available_ecus,
      "freeze_frame_positive_dtcs": freeze_frame_positive_dtcs,
      "freeze_frame_records": freeze_frame_records,
      "rob_available_ecus": rob_available_ecus,
      "rob_behavior_codes": rob_behavior_codes,
      "fault_status_records": fault_count,
    },
    "coverage": {
      "mount_support": "implemented for recovered family-local support modes",
      "dtc": "UDS ReadDTCInformation DTC_BY_STATUS_MASK over responding routed ECUs",
      "identity": "exact exported generic_cid request where available" if include_identities else "disabled by caller",
      "generic_ffd": (
        "raw ordinary-P5 per-DTC snapshots via exact exported role-0xB5/selector-0xCF contract; "
        "signal-level freeze-frame decoding not yet implemented"
      ),
      "info_code": "not_implemented",
      "operation_history": (
        "current-P5 RoB behavior-code inventory via exact exported role-0xA0 AB01/AB11 contracts; "
        "per-behavior record bodies not yet retrieved"
      ),
      "monitor_data": "not_implemented",
      "timestamp_data": "not_implemented",
    },
    "ecus": ecus,
  }


def save(document: dict[str, Any], path: str | Path) -> Path:
  out = Path(path).expanduser()
  out.parent.mkdir(parents=True, exist_ok=True)
  out.write_text(json.dumps(document, indent=2, sort_keys=True) + "\n")
  return out


def load(path: str | Path) -> dict[str, Any]:
  source = Path(path).expanduser()
  try:
    document = json.loads(source.read_text())
  except (OSError, json.JSONDecodeError) as e:
    raise registry.RegistryError(f"cannot read Health Check snapshot {source}: {e}") from e
  if not isinstance(document, dict) or document.get("schema") != SCHEMA:
    raise registry.RegistryError(f"{source} is not a {SCHEMA} document")
  return document


def _ecu_identity(row: dict[str, Any]) -> tuple[int, int, int | None]:
  return int(row["category_id"]), int(row["address"]), None if row.get("sub_addr") is None else int(row["sub_addr"])


def _dtc_map(row: dict[str, Any]) -> dict[str, int]:
  dtc_state = row.get("dtc") if isinstance(row.get("dtc"), dict) else {}
  records = dtc_state.get("records") if isinstance(dtc_state.get("records"), list) else []
  return {str(item["code"]): int(item["status"]) for item in records if isinstance(item, dict) and item.get("code")}


def _freeze_frame_map(row: dict[str, Any]) -> dict[tuple[str, int, int], str]:
  out: dict[tuple[str, int, int], str] = {}
  freeze = row.get("freeze_frames") if isinstance(row.get("freeze_frames"), dict) else {}
  for dtc_row in freeze.get("dtcs", []) if isinstance(freeze.get("dtcs"), list) else []:
    if not isinstance(dtc_row, dict) or dtc_row.get("state") != "positive":
      continue
    code = str(dtc_row.get("dtc") or "")
    for record in dtc_row.get("records", []) if isinstance(dtc_row.get("records"), list) else []:
      if not isinstance(record, dict):
        continue
      record_number = int(record.get("record_number", -1))
      for item in record.get("identifiers", []) if isinstance(record.get("identifiers"), list) else []:
        if isinstance(item, dict) and item.get("did") is not None and item.get("data_hex") is not None:
          out[(code, record_number, int(item["did"]))] = str(item["data_hex"])
  return out


def _rob_code_set(row: dict[str, Any]) -> set[tuple[int, int]]:
  out: set[tuple[int, int]] = set()
  rob = row.get("rob") if isinstance(row.get("rob"), dict) else {}
  for group in rob.get("groups", []) if isinstance(rob.get("groups"), list) else []:
    if not isinstance(group, dict) or group.get("state") != "positive":
      continue
    subfunction = int(group.get("subfunction", -1))
    for code in group.get("behavior_codes", []) if isinstance(group.get("behavior_codes"), list) else []:
      out.add((subfunction, int(code)))
  return out


def compare(before: dict[str, Any], after: dict[str, Any]) -> dict[str, Any]:
  """Compare two Health Check snapshots without requiring Toyota binaries or live hardware."""
  if before.get("schema") != SCHEMA or after.get("schema") != SCHEMA:
    raise registry.RegistryError(f"Health Check comparison requires two {SCHEMA} documents")
  before_rows = {_ecu_identity(row): row for row in before.get("ecus", [])}
  after_rows = {_ecu_identity(row): row for row in after.get("ecus", [])}
  changes: list[dict[str, Any]] = []
  for identity in sorted(set(before_rows) | set(after_rows)):
    old = before_rows.get(identity)
    new = after_rows.get(identity)
    exemplar = new or old or {}
    entry: dict[str, Any] = {
      "category_id": identity[0], "address": identity[1], "sub_addr": identity[2],
      "key": exemplar.get("key"), "name": exemplar.get("name"),
    }
    if old is None:
      entry["change"] = "candidate_added"
      changes.append(entry)
      continue
    if new is None:
      entry["change"] = "candidate_removed"
      changes.append(entry)
      continue

    old_mount = (old.get("mount") or {}).get("live_state")
    new_mount = (new.get("mount") or {}).get("live_state")
    if old_mount != new_mount:
      entry["mount"] = {"before": old_mount, "after": new_mount}

    old_dtc = _dtc_map(old)
    new_dtc = _dtc_map(new)
    added = [{"code": code, "status": new_dtc[code]} for code in sorted(new_dtc.keys() - old_dtc.keys())]
    removed = [{"code": code, "status": old_dtc[code]} for code in sorted(old_dtc.keys() - new_dtc.keys())]
    status_changed = [
      {"code": code, "before": old_dtc[code], "after": new_dtc[code]}
      for code in sorted(old_dtc.keys() & new_dtc.keys()) if old_dtc[code] != new_dtc[code]
    ]
    if added or removed or status_changed:
      entry["dtcs"] = {"added": added, "removed": removed, "status_changed": status_changed}

    old_ffd = _freeze_frame_map(old)
    new_ffd = _freeze_frame_map(new)
    ffd_added = [
      {"dtc": key[0], "record_number": key[1], "did": key[2], "data_hex": new_ffd[key]}
      for key in sorted(new_ffd.keys() - old_ffd.keys())
    ]
    ffd_removed = [
      {"dtc": key[0], "record_number": key[1], "did": key[2], "data_hex": old_ffd[key]}
      for key in sorted(old_ffd.keys() - new_ffd.keys())
    ]
    ffd_changed = [
      {"dtc": key[0], "record_number": key[1], "did": key[2], "before": old_ffd[key], "after": new_ffd[key]}
      for key in sorted(old_ffd.keys() & new_ffd.keys()) if old_ffd[key] != new_ffd[key]
    ]
    if ffd_added or ffd_removed or ffd_changed:
      entry["freeze_frames"] = {"added": ffd_added, "removed": ffd_removed, "changed": ffd_changed}

    old_rob = _rob_code_set(old)
    new_rob = _rob_code_set(new)
    rob_added = [{"subfunction": key[0], "behavior_code": key[1]} for key in sorted(new_rob - old_rob)]
    rob_removed = [{"subfunction": key[0], "behavior_code": key[1]} for key in sorted(old_rob - new_rob)]
    if rob_added or rob_removed:
      entry["rob"] = {"added": rob_added, "removed": rob_removed}

    old_ident = old.get("identity") if isinstance(old.get("identity"), dict) else None
    new_ident = new.get("identity") if isinstance(new.get("identity"), dict) else None
    old_hex = old_ident.get("data_hex") if old_ident else None
    new_hex = new_ident.get("data_hex") if new_ident else None
    if old_hex != new_hex:
      entry["identity"] = {"before": old_hex, "after": new_hex}

    old_dtc_state = (old.get("dtc") or {}).get("state")
    new_dtc_state = (new.get("dtc") or {}).get("state")
    if old_dtc_state != new_dtc_state:
      entry["dtc_state"] = {"before": old_dtc_state, "after": new_dtc_state}

    if len(entry) > 5:
      changes.append(entry)

  return {
    "schema": "toyota-health-check-diff-v1",
    "vehicle_type": after.get("vehicle_type"),
    "before_captured_at": before.get("captured_at"),
    "after_captured_at": after.get("captured_at"),
    "summary": {
      "changed_ecus": len(changes),
      "mount_state_changes": sum("mount" in row for row in changes),
      "dtc_changes": sum("dtcs" in row for row in changes),
      "freeze_frame_changes": sum("freeze_frames" in row for row in changes),
      "rob_changes": sum("rob" in row for row in changes),
      "identity_changes": sum("identity" in row for row in changes),
    },
    "changes": changes,
  }


def render_diff(document: dict[str, Any]) -> str:
  summary = document["summary"]
  lines = [
    "Health Check changes",
    (
      f"ECUs changed: {summary['changed_ecus']}  mount: {summary['mount_state_changes']}  "
      f"dtc: {summary['dtc_changes']}  ffd: {summary.get('freeze_frame_changes', 0)}  "
      f"rob: {summary.get('rob_changes', 0)}  identity: {summary['identity_changes']}"
    ),
  ]
  if not document["changes"]:
    lines.append("no changes")
    return "\n".join(lines)
  for row in document["changes"]:
    endpoint = f"0x{row['address']:X}" + (f"/0x{row['sub_addr']:02X}" if row.get("sub_addr") is not None else "")
    lines.append(f"{row.get('name') or row.get('key') or '?'} {endpoint} cat={row['category_id']}")
    if row.get("change"):
      lines.append(f"  {row['change']}")
    if "mount" in row:
      lines.append(f"  mount: {row['mount']['before']} -> {row['mount']['after']}")
    if "dtc_state" in row:
      lines.append(f"  dtc state: {row['dtc_state']['before']} -> {row['dtc_state']['after']}")
    if "identity" in row:
      lines.append(f"  identity: {row['identity']['before']} -> {row['identity']['after']}")
    dtcs = row.get("dtcs") or {}
    for item in dtcs.get("added", []):
      lines.append(f"  + DTC {item['code']} status=0x{item['status']:02X}")
    for item in dtcs.get("removed", []):
      lines.append(f"  - DTC {item['code']} status=0x{item['status']:02X}")
    for item in dtcs.get("status_changed", []):
      lines.append(f"  ~ DTC {item['code']} 0x{item['before']:02X} -> 0x{item['after']:02X}")
    freeze = row.get("freeze_frames") or {}
    for item in freeze.get("added", []):
      lines.append(f"  + FFD {item['dtc']} rec=0x{item['record_number']:02X} DID=0x{item['did']:04X} {item['data_hex']}")
    for item in freeze.get("removed", []):
      lines.append(f"  - FFD {item['dtc']} rec=0x{item['record_number']:02X} DID=0x{item['did']:04X} {item['data_hex']}")
    for item in freeze.get("changed", []):
      lines.append(
        f"  ~ FFD {item['dtc']} rec=0x{item['record_number']:02X} DID=0x{item['did']:04X} "
        f"{item['before']} -> {item['after']}")
    rob = row.get("rob") or {}
    for item in rob.get("added", []):
      lines.append(f"  + RoB sub=0x{item['subfunction']:02X} code=0x{item['behavior_code']:04X}")
    for item in rob.get("removed", []):
      lines.append(f"  - RoB sub=0x{item['subfunction']:02X} code=0x{item['behavior_code']:04X}")
  return "\n".join(lines)


def render(document: dict[str, Any]) -> str:
  summary = document["summary"]
  lines = [
    f"{document['vehicle']} Health Check",
    f"Toyota type {document['vehicle_type']}  region={document.get('region')}  logical bus={document.get('logical_bus')}",
    (
      f"Installed candidates: {summary['install_candidates']}  responding: {summary['mount_responding']}  "
      f"DTC responders: {summary['dtc_positive_ecus']}  faults: {summary['fault_status_records']}  "
      f"FFD records: {summary.get('freeze_frame_records', 0)}  RoB codes: {summary.get('rob_behavior_codes', 0)}"
    ),
    "",
  ]
  for row in document["ecus"]:
    mount = row.get("mount") or {}
    live_state = mount.get("live_state") or "unresolved"
    dtc_result = row["dtc"]
    mark = "!" if dtc_result["fault_count"] else ("✓" if live_state == "responding" else "-")
    endpoint = f"0x{row['address']:X}" + (f"/0x{row['sub_addr']:02X}" if row.get("sub_addr") is not None else "")
    ident = row.get("identity") or {}
    ident_text = ident.get("ascii") or (ident.get("data_hex") if ident.get("state") == "positive" else None)
    suffix = f"  {ident_text}" if ident_text else ""
    lines.append(
      f"{mark} {row['name']:<38} {endpoint:<14} cat={row['category_id']:<5} "
      f"mount={live_state:<17} dtc={dtc_result['state']}{suffix}"
    )
    for fault in dtc_result["records"]:
      if not fault["fault_status"]:
        continue
      description = fault["descriptions"][0] if fault["descriptions"] else {}
      name = description.get("description") or fault["code"]
      failure = description.get("failure")
      lines.append(f"    {fault['code']} status=0x{fault['status']:02X} {name}" + (f" — {failure}" if failure else ""))
  pending = [key for key, value in document["coverage"].items() if value == "not_implemented"]
  if pending:
    lines.extend(("", "GTS+ Health Check parity still pending: " + ", ".join(pending)))
  return "\n".join(lines)
