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

from toyota_diag import decode, dtc, recorder, registry, resolver, transport
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


def _command_requests(command: dict[str, Any] | None) -> list[dict[str, Any]]:
  if command is None:
    return []
  rows = [row for row in command.get("requests", []) if isinstance(row, dict) and row.get("resolved")]
  for protocol in command.get("protocols", []) if isinstance(command.get("protocols"), list) else []:
    if not isinstance(protocol, dict):
      continue
    for phase in ("inventory", "frames", "record"):
      row = protocol.get(phase)
      if isinstance(row, dict) and row.get("resolved"):
        rows.append(row)
  return rows


def _p5_rob_plan(profile: registry.Profile, ecu: registry.EcuSpec) -> dict[str, Any] | None:
  """Return the exact exported ordinary-P5 RoB inventory/frame/record command."""
  command = _catalog_command(profile, ecu, "p5_rob")
  if command is None or command.get("execution") != "read_only":
    return None
  binding = command.get("plugin_binding")
  if not isinstance(binding, dict) or binding.get("dll") != "GetRoBP5_DT.dll" or binding.get("exact_category_binding") is not True:
    return None
  protocols = command.get("protocols")
  if not isinstance(protocols, list) or len(protocols) != 2:
    return None
  expected = [
    (("ab01", "eb01"), ("ab020000", "eb02"), ("ab0300000000", "eb03")),
    (("ab11", "eb11"), ("ab120000", "eb12"), ("ab1300000000", "eb13")),
  ]
  actual = []
  try:
    for protocol in protocols:
      if not isinstance(protocol, dict):
        return None
      phases = []
      for phase in ("inventory", "frames", "record"):
        row = protocol.get(phase)
        if not isinstance(row, dict) or row.get("resolved") is not True:
          return None
        phases.append((
          registry.parse_bytes(row.get("send"), f"P5 RoB {phase} request").hex(),
          registry.parse_bytes(row.get("check"), f"P5 RoB {phase} positive check").hex(),
        ))
      actual.append(tuple(phases))
  except registry.RegistryError:
    return None
  if actual != expected:
    return None
  response = command.get("response_model")
  if not isinstance(response, dict):
    return None
  record = response.get("record")
  if not isinstance(record, dict) or record.get("payload_offset") != 6 or record.get("block_count_offset") != 6:
    return None
  return command


def _raw_request(client, request: bytes) -> tuple[str, bytes | None, str | None]:
  try:
    response = bytes(transport.raw_isotp(client, request))
  except MessageTimeoutError:
    return "no_response", None, None
  except Exception as e:
    return "error", None, str(e)
  if len(response) >= 3 and response[0] == 0x7F:
    return "negative_response", response, f"NRC 0x{response[2]:02X}"
  return "positive", response, None


def _rob_semantic_indexes(metadata: dict[str, Any] | None) -> tuple[dict[int, dict[str, Any]], dict[int, list[dict[str, Any]]]]:
  if not isinstance(metadata, dict):
    return {}, {}
  behavior_by_code: dict[int, dict[str, Any]] = {}
  for row in metadata.get("behavior_codes", []) if isinstance(metadata.get("behavior_codes"), list) else []:
    if isinstance(row, dict) and row.get("behavior_code") is not None:
      behavior_by_code.setdefault(int(row["behavior_code"]), row)
  signals_by_did: dict[int, list[dict[str, Any]]] = {}
  for row in metadata.get("signals", []) if isinstance(metadata.get("signals"), list) else []:
    if isinstance(row, dict) and row.get("did") is not None:
      signals_by_did.setdefault(int(row["did"]), []).append(row)
  for rows in signals_by_did.values():
    rows.sort(key=lambda row: (int(row.get("sort_key") or 0), int(row.get("record_key") or 0)))
  return behavior_by_code, signals_by_did


def _decode_p5_rob_block(block: dict[str, Any], signals_by_did: dict[int, list[dict[str, Any]]]) -> dict[str, Any]:
  did = int(block["data_id"])
  data = bytes(block["data"])
  rows = signals_by_did.get(did, [])
  decoded_signals: list[dict[str, Any]] = []
  decode_errors: list[str] = []
  suppressed = 0
  for row in rows:
    try:
      decoded = decode.decode_rob_signal(data, row)
    except decode.DecodeError as e:
      decode_errors.append(f"{row.get('name') or 'unnamed'}: {e}")
      continue
    if decoded.get("state") == "not_supported":
      suppressed += 1
      continue
    decoded.update({
      "bit_start": int(row["bit_start"]),
      "bit_end": int(row["bit_end"]),
      "extraction_mode": int(row.get("extraction_mode") or 0),
    })
    decoded_signals.append(decoded)
  if not rows:
    state = "no_schema"
  elif decode_errors:
    state = "partial" if decoded_signals else "decode_error"
  else:
    state = "decoded"
  return {
    "did": did,
    "length": int(block["length"]),
    "data_hex": data.hex(),
    "signal_decode": state,
    "signals": decoded_signals,
    "suppressed_signal_count": suppressed,
    "decode_errors": decode_errors,
  }


def _p5_rob_from_client(command: dict[str, Any] | None, client, metadata: dict[str, Any] | None = None) -> dict[str, Any]:
  if command is None:
    return {
      "state": "not_available", "groups": [], "behavior_code_count": 0,
      "unique_behavior_code_count": 0, "frame_count": 0, "record_count": 0, "did_block_count": 0,
      "decoded_signal_count": 0, "suppressed_signal_count": 0, "decode_error_count": 0,
    }

  behavior_by_code, signals_by_did = _rob_semantic_indexes(metadata)
  groups: list[dict[str, Any]] = []
  all_codes: list[int] = []
  frame_count = 0
  record_count = 0
  did_block_count = 0
  decoded_signal_count = 0
  suppressed_signal_count = 0
  decode_error_count = 0

  for protocol in command.get("protocols", []):
    inventory_row = protocol["inventory"]
    frames_row = protocol["frames"]
    record_row = protocol["record"]
    try:
      inventory_request = registry.parse_bytes(inventory_row["send"], "P5 RoB inventory request")
      inventory_check = registry.parse_bytes(inventory_row["check"], "P5 RoB inventory check")
      frames_base = registry.parse_bytes(frames_row["send"], "P5 RoB frames request")
      frames_check = registry.parse_bytes(frames_row["check"], "P5 RoB frames check")
      record_base = registry.parse_bytes(record_row["send"], "P5 RoB record request")
      record_check = registry.parse_bytes(record_row["check"], "P5 RoB record check")
    except (KeyError, registry.RegistryError) as e:
      groups.append({"state": "error", "error": str(e), "behavior_codes": [], "behaviors": []})
      continue

    state, response, error = _raw_request(client, inventory_request)
    group: dict[str, Any] = {
      "inventory_subfunction": inventory_request[1],
      "frame_subfunction": frames_base[1],
      "record_subfunction": record_base[1],
      "state": state,
      "behavior_codes": [],
      "behaviors": [],
    }
    if response is not None:
      group["inventory_response_hex"] = response.hex()
    if error is not None:
      group["error"] = error
    if state != "positive" or response is None:
      groups.append(group)
      continue
    try:
      codes = recorder.parse_p5_rob_behavior_codes(response, inventory_check)
    except recorder.RecorderError as e:
      group.update(state="parse_error", error=str(e))
      groups.append(group)
      continue

    group["behavior_codes"] = codes
    all_codes.extend(codes)
    for behavior in codes:
      frame_request = bytearray(frames_base)
      frame_request[2:4] = int(behavior).to_bytes(2, "big")
      frame_state, frame_response, frame_error = _raw_request(client, bytes(frame_request))
      behavior_row: dict[str, Any] = {
        "behavior_code": int(behavior), "state": frame_state, "frames": [],
      }
      behavior_meta = behavior_by_code.get(int(behavior))
      if behavior_meta is not None:
        behavior_row.update({
          "behavior_signature": behavior_meta.get("signature"),
          "behavior_name": behavior_meta.get("name"),
          "behavior_comment": behavior_meta.get("comment"),
        })
      if frame_response is not None:
        behavior_row["frame_response_hex"] = frame_response.hex()
      if frame_error is not None:
        behavior_row["error"] = frame_error
      if frame_state != "positive" or frame_response is None:
        group["behaviors"].append(behavior_row)
        continue
      try:
        parsed_frames = recorder.parse_p5_rob_frames(frame_response, frames_check)
      except recorder.RecorderError as e:
        behavior_row.update(state="parse_error", error=str(e))
        group["behaviors"].append(behavior_row)
        continue
      behavior_row["behavior_echo"] = parsed_frames["behavior_echo"]
      frame_ids = list(parsed_frames["frames"])
      frame_count += len(frame_ids)
      for frame_id in frame_ids:
        record_request = bytearray(record_base)
        record_request[2:4] = int(behavior).to_bytes(2, "big")
        record_request[4:6] = int(frame_id).to_bytes(2, "big")
        rec_state, rec_response, rec_error = _raw_request(client, bytes(record_request))
        frame_row: dict[str, Any] = {"frame_id": int(frame_id), "state": rec_state}
        if rec_response is not None:
          frame_row["response_hex"] = rec_response.hex()
        if rec_error is not None:
          frame_row["error"] = rec_error
        if rec_state == "positive" and rec_response is not None:
          try:
            parsed_record = recorder.parse_p5_rob_record(rec_response, record_check)
          except recorder.RecorderError as e:
            frame_row.update(state="parse_error", error=str(e))
          else:
            blocks = [_decode_p5_rob_block(block, signals_by_did) for block in parsed_record["blocks"]]
            decoded_signal_count += sum(len(block["signals"]) for block in blocks)
            suppressed_signal_count += sum(int(block["suppressed_signal_count"]) for block in blocks)
            decode_error_count += sum(len(block["decode_errors"]) for block in blocks)
            frame_row["record"] = {
              "behavior_echo": parsed_record["behavior_echo"],
              "frame_echo": parsed_record["frame_echo"],
              "declared_block_count": parsed_record["declared_block_count"],
              "block_count": parsed_record["block_count"],
              "blocks": blocks,
            }
            record_count += 1
            did_block_count += len(blocks)
        behavior_row["frames"].append(frame_row)
      group["behaviors"].append(behavior_row)
    groups.append(group)

  return {
    "state": "available",
    "groups": groups,
    "behavior_code_count": len(all_codes),
    "unique_behavior_code_count": len(set(all_codes)),
    "frame_count": frame_count,
    "record_count": record_count,
    "did_block_count": did_block_count,
    "decoded_signal_count": decoded_signal_count,
    "suppressed_signal_count": suppressed_signal_count,
    "decode_error_count": decode_error_count,
    "signal_decode": "gts_current_p5" if signals_by_did else "raw_only",
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
  rob_plan = _p5_rob_plan(profile, ecu)
  category = profile.category(ecu) or {}
  rob_metadata = category.get("rob") if isinstance(category.get("rob"), dict) else None
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
    "frame_count": 0, "record_count": 0, "did_block_count": 0,
    "decoded_signal_count": 0, "suppressed_signal_count": 0, "decode_error_count": 0,
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
      requests = [request for plan in plans for request in _command_requests(plan)]
      if any(request.get("session_requirement") == SESSION_REQUIREMENT_EXTENDED for request in requests):
        session.enter_extended()
      client = session.client()
      if needs_identity:
        identity = _identity_from_client(identity_plan, client)
      if needs_ffd:
        freeze_frames = _freeze_frames_from_client(ffd_plan, client, dtc_rows)
      if needs_rob:
        rob = _p5_rob_from_client(rob_plan, client, rob_metadata)
  except Exception as e:
    message = str(e)
    if needs_identity and identity is None:
      _, did = identity_plan
      identity = {"did": did, "state": "error", "data_hex": None, "ascii": None, "error": message}
    if needs_ffd and freeze_frames is None:
      freeze_frames = {"state": "error", "dtcs": [], "positive_dtc_count": 0, "record_count": 0, "error": message}
    if needs_rob and rob is None:
      rob = {
        "state": "error", "groups": [], "behavior_code_count": 0, "unique_behavior_code_count": 0,
        "frame_count": 0, "record_count": 0, "did_block_count": 0,
        "decoded_signal_count": 0, "suppressed_signal_count": 0, "decode_error_count": 0,
        "error": message,
      }
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
  rob_frames = 0
  rob_records = 0
  rob_did_blocks = 0
  rob_decoded_signals = 0
  rob_suppressed_signals = 0
  rob_decode_errors = 0

  for ecu in profile.ecus:
    mount = mount_lookup.get(ecu.endpoint)
    responding = bool(mount and mount.get("transport_responded") is True)
    dtc_result = {"state": "not_queried", "records": [], "fault_count": 0}
    identity = None
    freeze_frames = {"state": "not_queried", "dtcs": [], "positive_dtc_count": 0, "record_count": 0}
    rob = {
      "state": "not_queried", "groups": [], "behavior_code_count": 0, "unique_behavior_code_count": 0,
      "frame_count": 0, "record_count": 0, "did_block_count": 0,
      "decoded_signal_count": 0, "suppressed_signal_count": 0, "decode_error_count": 0,
    }
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
      rob_frames += int(rob.get("frame_count", 0))
      rob_records += int(rob.get("record_count", 0))
      rob_did_blocks += int(rob.get("did_block_count", 0))
      rob_decoded_signals += int(rob.get("decoded_signal_count", 0))
      rob_suppressed_signals += int(rob.get("suppressed_signal_count", 0))
      rob_decode_errors += int(rob.get("decode_error_count", 0))

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
      "rob_frames": rob_frames,
      "rob_records": rob_records,
      "rob_did_blocks": rob_did_blocks,
      "rob_decoded_signals": rob_decoded_signals,
      "rob_suppressed_signals": rob_suppressed_signals,
      "rob_decode_errors": rob_decode_errors,
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
        "current-P5 RoB behavior/frame/record transport plus OEM behavior names and DID-scoped signal decoding "
        "from the exported current GTS+ type-87/88/90 + physical/unit/pattern schema"
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
    subfunction = int(group.get("inventory_subfunction", group.get("subfunction", -1)))
    for code in group.get("behavior_codes", []) if isinstance(group.get("behavior_codes"), list) else []:
      out.add((subfunction, int(code)))
  return out


def _rob_record_map(row: dict[str, Any]) -> dict[tuple[int, int, int, int], str]:
  out: dict[tuple[int, int, int, int], str] = {}
  rob = row.get("rob") if isinstance(row.get("rob"), dict) else {}
  for group in rob.get("groups", []) if isinstance(rob.get("groups"), list) else []:
    if not isinstance(group, dict) or group.get("state") != "positive":
      continue
    subfunction = int(group.get("inventory_subfunction", group.get("subfunction", -1)))
    for behavior in group.get("behaviors", []) if isinstance(group.get("behaviors"), list) else []:
      if not isinstance(behavior, dict):
        continue
      code = int(behavior.get("behavior_code", -1))
      for frame in behavior.get("frames", []) if isinstance(behavior.get("frames"), list) else []:
        if not isinstance(frame, dict) or frame.get("state") != "positive":
          continue
        frame_id = int(frame.get("frame_id", -1))
        record = frame.get("record") if isinstance(frame.get("record"), dict) else {}
        for block in record.get("blocks", []) if isinstance(record.get("blocks"), list) else []:
          if isinstance(block, dict) and block.get("did") is not None and block.get("data_hex") is not None:
            out[(subfunction, code, frame_id, int(block["did"]))] = str(block["data_hex"])
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
    old_rob_records = _rob_record_map(old)
    new_rob_records = _rob_record_map(new)
    rob_record_added = [
      {"subfunction": key[0], "behavior_code": key[1], "frame_id": key[2], "did": key[3], "data_hex": new_rob_records[key]}
      for key in sorted(new_rob_records.keys() - old_rob_records.keys())
    ]
    rob_record_removed = [
      {"subfunction": key[0], "behavior_code": key[1], "frame_id": key[2], "did": key[3], "data_hex": old_rob_records[key]}
      for key in sorted(old_rob_records.keys() - new_rob_records.keys())
    ]
    rob_record_changed = [
      {"subfunction": key[0], "behavior_code": key[1], "frame_id": key[2], "did": key[3],
       "before": old_rob_records[key], "after": new_rob_records[key]}
      for key in sorted(old_rob_records.keys() & new_rob_records.keys()) if old_rob_records[key] != new_rob_records[key]
    ]
    if rob_added or rob_removed or rob_record_added or rob_record_removed or rob_record_changed:
      entry["rob"] = {
        "added": rob_added, "removed": rob_removed,
        "records": {"added": rob_record_added, "removed": rob_record_removed, "changed": rob_record_changed},
      }

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
    rob_records = rob.get("records") or {}
    for item in rob_records.get("added", []):
      lines.append(
        f"  + RoB data sub=0x{item['subfunction']:02X} code=0x{item['behavior_code']:04X} "
        f"frame=0x{item['frame_id']:04X} DID=0x{item['did']:04X} {item['data_hex']}")
    for item in rob_records.get("removed", []):
      lines.append(
        f"  - RoB data sub=0x{item['subfunction']:02X} code=0x{item['behavior_code']:04X} "
        f"frame=0x{item['frame_id']:04X} DID=0x{item['did']:04X} {item['data_hex']}")
    for item in rob_records.get("changed", []):
      lines.append(
        f"  ~ RoB data sub=0x{item['subfunction']:02X} code=0x{item['behavior_code']:04X} "
        f"frame=0x{item['frame_id']:04X} DID=0x{item['did']:04X} {item['before']} -> {item['after']}")
  return "\n".join(lines)


def render(document: dict[str, Any]) -> str:
  summary = document["summary"]
  lines = [
    f"{document['vehicle']} Health Check",
    f"Toyota type {document['vehicle_type']}  region={document.get('region')}  logical bus={document.get('logical_bus')}",
    (
      f"Installed candidates: {summary['install_candidates']}  responding: {summary['mount_responding']}  "
      f"DTC responders: {summary['dtc_positive_ecus']}  faults: {summary['fault_status_records']}  "
      f"FFD records: {summary.get('freeze_frame_records', 0)}  RoB records: {summary.get('rob_records', 0)}  "
      f"RoB signals: {summary.get('rob_decoded_signals', 0)}"
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
