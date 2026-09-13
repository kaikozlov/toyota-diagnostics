"""Toyota GTS+ Customize catalog resolution and current P5/P6 execution."""
from __future__ import annotations

from dataclasses import dataclass
from typing import Any

from toyota_diag import decode, registry, resolver
from toyota_diag.registry import EcuSpec, Profile


class CustomizeError(ValueError):
  pass


@dataclass(frozen=True)
class CustomizeTarget:
  item: dict[str, Any]
  ecu: EcuSpec
  phase_type: int
  write_did: int
  family: str


def catalog(profile: Profile) -> dict[str, Any]:
  if profile.database is None:
    raise CustomizeError("Customize requires the universal Toyota diagnostic bundle")
  try:
    return profile.database.customize_catalog(profile.region)
  except registry.RegistryError as e:
    raise CustomizeError(str(e)) from e


def group_rows(profile: Profile, group: str | int, *, body_type: int | None = None) -> list[dict[str, Any]]:
  rows = [row for row in catalog(profile).get("groups", []) if isinstance(row, dict)]
  if body_type is not None:
    rows = [row for row in rows if int(row.get("body_type", -1)) == body_type]
  try:
    numeric = int(group) if isinstance(group, int) else int(str(group), 0)
  except ValueError:
    numeric = None
  if numeric is not None:
    matches = [row for row in rows if int(row.get("group_id", -1)) == numeric]
  else:
    needle = str(group).casefold()
    exact = [row for row in rows if str(row.get("name") or "").casefold() == needle]
    matches = exact or [row for row in rows if needle in str(row.get("name") or "").casefold()]
  ids = {int(row["group_id"]) for row in matches}
  if not matches:
    raise CustomizeError(f"no Customize group matches {group!r}")
  if len(ids) != 1:
    summary = ", ".join(f"{row['group_id']} {row.get('name') or ''} body={row.get('body_type')}" for row in matches[:12])
    raise CustomizeError(f"ambiguous Customize group {group!r}: {summary}")
  return matches


def lookup_item(profile: Profile, group: str | int, item: str | int, *, body_type: int | None = None) -> dict[str, Any]:
  groups = group_rows(profile, group, body_type=body_type)
  group_id = int(groups[0]["group_id"])
  rows = [row for row in catalog(profile).get("items", [])
          if isinstance(row, dict) and int(row.get("group_id", -1)) == group_id]
  try:
    numeric = int(item) if isinstance(item, int) else int(str(item), 0)
  except ValueError:
    numeric = None
  if numeric is not None:
    matches = [row for row in rows if int(row.get("item_id", -1)) == numeric]
  else:
    needle = str(item).casefold()
    exact = [row for row in rows if str(row.get("name") or "").casefold() == needle]
    matches = exact or [row for row in rows if needle in str(row.get("name") or "").casefold()]
  if len(matches) == 1:
    return dict(matches[0])
  if not matches:
    raise CustomizeError(f"no Customize item in group {group_id} matches {item!r}")
  installed_categories = {ecu.category_id for ecu in profile.ecus if ecu.category_id is not None}
  installed = [row for row in matches if int(row.get("target_category_id", -1)) in installed_categories]
  if len(installed) == 1:
    return dict(installed[0])
  candidates = installed or matches
  summary = ", ".join(
    f"{row['item_id']} {row.get('name') or ''} target={row.get('target_category_id')}" for row in candidates[:12])
  raise CustomizeError(f"ambiguous Customize item {item!r}: {summary}")


def resolve_target(profile: Profile, item: dict[str, Any]) -> CustomizeTarget:
  try:
    category_id = registry.parse_int(item["target_category_id"], "Customize target_category_id")
    phase_type = registry.parse_int(
      item.get("target_phase_type"), "Customize target_phase_type")
    write_did = registry.parse_int(item.get("write_did"), "Customize write_did")
  except (KeyError, registry.RegistryError) as e:
    raise CustomizeError(f"Customize item has incomplete target geometry: {e}") from e
  if write_did <= 0 or write_did > 0xFFFF:
    raise CustomizeError(f"Customize item has invalid write DID 0x{write_did:X}")

  matches = [ecu for ecu in profile.ecus if ecu.category_id == category_id]
  if not matches:
    raise CustomizeError(
      f"Customize target category {category_id} {item.get('target_category_name') or ''} is not installed on selected vehicle")
  if len(matches) != 1:
    raise CustomizeError(f"Customize target category {category_id} resolves {len(matches)} mounted logical ECUs")
  ecu = matches[0]

  try:
    candidate, _route = resolver.lookup_mount_candidate(profile, category_id)
  except resolver.ResolverError as e:
    raise CustomizeError(str(e)) from e
  mounted_phase = registry.parse_int(candidate.get("connection_phase_type"), "Customize mounted phase_type")
  if mounted_phase != phase_type:
    raise CustomizeError(
      f"Customize item phase 0x{phase_type:02X} disagrees with selected vehicle route phase 0x{mounted_phase:02X}")

  family = resolver.support_family(profile, category_id) or "unresolved"
  mode = resolver.support_mode(profile, category_id)
  if mode not in {"p5-standard", "p6-standard"}:
    raise CustomizeError(
      f"Customize target category {category_id} selects support mode {mode or family}; current write executor covers standard P5/P6 only")
  return CustomizeTarget(item=item, ecu=ecu, phase_type=phase_type, write_did=write_did, family=family)


def resolve_choice(item: dict[str, Any], value: str | int) -> tuple[int, str | None]:
  choices = [row for row in item.get("choices", []) if isinstance(row, dict)]
  if isinstance(value, int):
    numeric = value
  else:
    text = str(value).strip()
    try:
      numeric = int(text, 0)
    except ValueError:
      needle = text.casefold()
      exact = [row for row in choices if str(row.get("name") or "").casefold() == needle]
      matches = exact or [row for row in choices if needle in str(row.get("name") or "").casefold()]
      if len(matches) == 1:
        return int(matches[0]["value"]), str(matches[0].get("name") or "") or None
      if not matches:
        raise CustomizeError(f"no OEM Customize choice matches {value!r}")
      raise CustomizeError(f"ambiguous OEM Customize choice {value!r}")
  matches = [row for row in choices if int(row.get("value", -1)) == numeric]
  if choices and not matches:
    valid = ", ".join(f"{row.get('name')}={row.get('value')}" for row in choices)
    raise CustomizeError(f"Customize value {numeric} is not an OEM choice; valid: {valid}")
  name = str(matches[0].get("name") or "") if len(matches) == 1 else None
  return numeric, name or None


def extract_current_value(payload: bytes, item: dict[str, Any]) -> int:
  try:
    start = registry.parse_int(item["current_bit_start"], "Customize current_bit_start")
    end = registry.parse_int(item["current_bit_end"], "Customize current_bit_end")
  except (KeyError, registry.RegistryError) as e:
    raise CustomizeError(str(e)) from e
  try:
    return decode.extract_msb0(payload, start, end)
  except decode.DecodeError as e:
    raise CustomizeError(f"Customize current value cannot be decoded: {e}") from e


def merge_value(payload: bytes, item: dict[str, Any], value: int) -> bytes:
  """Reproduce SetCustomBase's MSB0 clear-mask + inserted-value merge."""
  try:
    start = registry.parse_int(item["write_bit_start"], "Customize write_bit_start")
    end = registry.parse_int(item["write_bit_end"], "Customize write_bit_end")
    mode = registry.parse_int(item["merge_mode"], "Customize write support mode")
  except (KeyError, registry.RegistryError) as e:
    raise CustomizeError(str(e)) from e
  if mode not in (0, 1):
    raise CustomizeError(f"Customize write merge mode {mode} is not recovered")
  if start < 0 or end < start or end >= len(payload) * 8:
    raise CustomizeError(f"Customize write bits {start}..{end} do not fit {len(payload)}-byte current value")
  width = end - start + 1
  if value < 0 or value >= (1 << width):
    raise CustomizeError(f"Customize value {value} does not fit {width}-bit field")

  if mode == 1:
    # Current NA/EU/JP modern Customize corpus has 287 mode-1 rows and every one
    # is a one-bit field. The current SetCustom helper checks that same bit position
    # in the immediately preceding byte before allowing the write.
    if width != 1:
      raise CustomizeError(f"Customize mode-1 {width}-bit geometry is outside the recovered current corpus")
    support_byte = (end >> 3) - 1
    if support_byte < 0:
      raise CustomizeError("Customize mode-1 write field has no preceding support byte")
    support_mask = 0x80 >> (end & 7)
    if payload[support_byte] & support_mask != support_mask:
      raise CustomizeError("Customize field is not supported in the current ECU value buffer")

  out = bytearray(payload)
  for bit_offset in range(width):
    absolute = end - bit_offset
    mask = 1 << (7 - (absolute & 7))
    byte_index = absolute >> 3
    if value & (1 << bit_offset):
      out[byte_index] |= mask
    else:
      out[byte_index] &= ~mask & 0xFF
  return bytes(out)


def read_current(profile: Profile, target: CustomizeTarget, client: Any) -> dict[str, Any]:
  support = resolver.did_support_resolver(profile, target.ecu.category_id, client)
  if not support.supports(target.write_did):
    raise CustomizeError(
      f"Toyota support inventory does not advertise Customize DID 0x{target.write_did:04X} on {target.ecu.name}")
  payload = bytes(client.read_data_by_identifier(target.write_did))
  value = extract_current_value(payload, target.item)
  choice = next((row for row in target.item.get("choices", [])
                 if isinstance(row, dict) and int(row.get("value", -1)) == value), None)
  return {
    "payload": payload,
    "value": value,
    "choice": str(choice.get("name") or "") if choice else None,
  }


def set_value(profile: Profile, target: CustomizeTarget, client: Any, requested: int) -> dict[str, Any]:
  before = read_current(profile, target, client)
  merged = merge_value(before["payload"], target.item, requested)
  if merged == before["payload"]:
    return {"before": before, "written": merged, "after": before, "changed": False}
  client.write_data_by_identifier(target.write_did, merged)
  after_payload = bytes(client.read_data_by_identifier(target.write_did))
  after_value = extract_current_value(after_payload, target.item)
  if after_payload != merged or after_value != requested:
    raise CustomizeError(
      f"Customize post-write verification failed: expected {merged.hex()} value {requested}, got {after_payload.hex()} value {after_value}")
  choice = next((row for row in target.item.get("choices", [])
                 if isinstance(row, dict) and int(row.get("value", -1)) == after_value), None)
  return {
    "before": before,
    "written": merged,
    "after": {
      "payload": after_payload,
      "value": after_value,
      "choice": str(choice.get("name") or "") if choice else None,
    },
    "changed": True,
  }
