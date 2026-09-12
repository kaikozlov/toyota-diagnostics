"""Pure decoders for GTS-derived Toyota diagnostic signal metadata."""
from __future__ import annotations

from typing import Any

P5_LINEAR_MSB0_V1 = "p5-linear-msb0-v1"


class DecodeError(ValueError):
  pass


def extract_msb0(payload: bytes, bit_start: int, bit_end: int) -> int:
  """Extract an inclusive Toyota Data Monitor field with MSB-first bit numbering."""
  if bit_start < 0 or bit_end < bit_start:
    raise DecodeError(f"invalid bit range {bit_start}..{bit_end}")
  if bit_end >= len(payload) * 8:
    raise DecodeError(f"bits {bit_start}..{bit_end} exceed {len(payload)}-byte DID payload")
  start_byte = bit_start >> 3
  end_byte = bit_end >> 3
  assembled = int.from_bytes(payload[start_byte:end_byte + 1], "big")
  shift = 7 - (bit_end & 7)
  width = bit_end - bit_start + 1
  return (assembled >> shift) & ((1 << width) - 1)


def _trunc_div_toward_zero(numerator: int, denominator: int) -> int:
  if denominator == 0:
    raise DecodeError("Toyota physical conversion divisor is zero")
  quotient = abs(numerator) // abs(denominator)
  return -quotient if (numerator < 0) != (denominator < 0) else quotient


def convert_p5_physical(raw: int, *, bit_width: int, signed: bool, mul: int, div: int, offset: int) -> int:
  if bit_width <= 0:
    raise DecodeError(f"invalid bit width {bit_width}")
  mask = (1 << bit_width) - 1
  value = raw & mask
  if signed and value & (1 << (bit_width - 1)):
    value -= 1 << bit_width
  numerator = value * mul
  converted = numerator if div <= 1 else _trunc_div_toward_zero(numerator, div)
  return converted + offset


def format_p5_decimal(converted_integer: int, decimal_point_count: int) -> str:
  if decimal_point_count < 0:
    raise DecodeError(f"invalid decimal point count {decimal_point_count}")
  if decimal_point_count == 0:
    return str(converted_integer)
  scale = 10 ** decimal_point_count
  magnitude = abs(converted_integer)
  whole, fraction = divmod(magnitude, scale)
  sign = "-" if converted_integer < 0 else ""
  return f"{sign}{whole}.{fraction:0{decimal_point_count}d}"


def decode_signal(payload: bytes, row: dict[str, Any]) -> dict[str, Any]:
  decoder = row.get("decoder")
  if decoder != P5_LINEAR_MSB0_V1:
    raise DecodeError(f"unsupported decoder {decoder!r}")

  try:
    bit_start = int(row["bit_start"])
    bit_end = int(row["bit_end"])
    mul = int(row["mul"])
    div = int(row["div"])
    offset = int(row["offset"])
    decimal_point_count = int(row["decimal_point_count"])
  except (KeyError, TypeError, ValueError) as e:
    raise DecodeError(f"incomplete {P5_LINEAR_MSB0_V1} metadata") from e

  raw = extract_msb0(payload, bit_start, bit_end)
  converted = convert_p5_physical(
    raw,
    bit_width=bit_end - bit_start + 1,
    signed=bool(row.get("signed", False)),
    mul=mul,
    div=div,
    offset=offset,
  )
  patterns = row.get("patterns") or {}
  pattern = patterns.get(str(converted))
  return {
    "raw": raw,
    "converted_integer": converted,
    "value": format_p5_decimal(converted, decimal_point_count),
    "pattern": pattern,
  }


def format_decoded_signal(payload: bytes, row: dict[str, Any]) -> str:
  result = decode_signal(payload, row)
  name = str(row.get("name") or "(unnamed)")
  width = int(row["bit_end"]) - int(row["bit_start"]) + 1
  raw_digits = max(1, (width + 3) // 4)
  raw_text = f"0x{result['raw']:0{raw_digits}X}"
  if result["pattern"] is not None:
    rendered = str(result["pattern"])
  else:
    rendered = str(result["value"])
    unit = row.get("unit")
    if unit:
      rendered += f" {unit}"
  return f"{name}: {rendered} (raw={raw_text})"


def p5_local_supported(payload: bytes, row: dict[str, Any]) -> bool:
  """Apply current P5 type-61/type-90 local support semantics for one signal row."""
  try:
    bit_end = int(row["bit_end"])
    mode = int(row["local_support_mode"])
  except (KeyError, TypeError, ValueError) as e:
    raise DecodeError("incomplete current-P5 RoB support metadata") from e
  if bit_end >= len(payload) * 8:
    return False
  if mode in (0, 2):
    return True
  if mode == 1:
    byte_index = (bit_end >> 3) - 1
    if byte_index < 0 or byte_index >= len(payload):
      return False
    mask = 0x80 >> (bit_end & 7)
    return (payload[byte_index] & mask) == mask
  raise DecodeError(f"unsupported current-P5 RoB local support mode {mode}")


def rob_local_supported(payload: bytes, row: dict[str, Any]) -> bool:
  return p5_local_supported(payload, row)


def _decode_p5_exported_signal(payload: bytes, row: dict[str, Any]) -> dict[str, Any]:
  info = row.get("signal_info")
  if not isinstance(info, dict):
    raise DecodeError("signal has no exported physical metadata")
  decoder_row = {
    "decoder": P5_LINEAR_MSB0_V1,
    "name": row.get("name"),
    "bit_start": row.get("bit_start"),
    "bit_end": row.get("bit_end"),
    "mul": info.get("mul"),
    "div": info.get("div"),
    "offset": info.get("offset"),
    "signed": info.get("signed", False),
    "decimal_point_count": info.get("decimal_point_count"),
    "patterns": info.get("pattern_display") or {},
    "unit": info.get("unit"),
  }
  decoded = decode_signal(payload, decoder_row)
  rendered = str(decoded["pattern"]) if decoded["pattern"] is not None else str(decoded["value"])
  unit = info.get("unit")
  if decoded["pattern"] is None and unit:
    rendered += f" {unit}"
  return {
    "state": "decoded",
    "name": row.get("name"),
    "raw": decoded["raw"],
    "converted_integer": decoded["converted_integer"],
    "value": decoded["value"],
    "pattern": decoded["pattern"],
    "unit": unit,
    "formatted": rendered,
  }


def decode_ffd_signal(
    payload: bytes,
    row: dict[str, Any],
    record_payloads: dict[int, bytes],
) -> dict[str, Any]:
  """Decode one current ordinary-P5 generic freeze-frame signal."""
  if bool(row.get("dynamic_lsb_possible")):
    raise DecodeError("FFD dynamic-LSB materialization is required before decoding this signal")
  if not p5_local_supported(payload, row):
    return {"state": "not_supported", "name": row.get("name")}

  condition = row.get("support_condition")
  if condition is not None:
    if not isinstance(condition, dict):
      raise DecodeError("invalid FFD support condition metadata")
    try:
      condition_type = int(condition["condition_type"])
      referenced_did = int(condition["referenced_did"])
      bit_start = int(condition["bit_start"])
      bit_end = int(condition["bit_end"])
    except (KeyError, TypeError, ValueError) as e:
      raise DecodeError("incomplete FFD support condition metadata") from e
    if condition_type != 7:
      raise DecodeError(f"unsupported FFD support condition type {condition_type}")
    referenced_payload = record_payloads.get(referenced_did)
    if referenced_payload is None:
      return {"state": "not_supported", "name": row.get("name")}
    if extract_msb0(referenced_payload, bit_start, bit_end) == 0:
      return {"state": "not_supported", "name": row.get("name")}

  decoded = _decode_p5_exported_signal(payload, row)
  decoded.update({
    "monitor_key": int(row.get("monitor_key") or 0),
    "sort_key": int(row.get("sort_key") or 0),
  })
  return decoded


def decode_rob_signal(payload: bytes, row: dict[str, Any]) -> dict[str, Any]:
  """Decode one current ordinary-P5 RoB signal from exported GTS+ metadata."""
  if int(row.get("support_condition_key") or 0) != 0:
    raise DecodeError("RoB cross-DID support condition metadata is required before decoding this signal")
  if bool(row.get("dynamic_lsb_possible")):
    raise DecodeError("RoB dynamic-LSB materialization is required before decoding this signal")
  if int(row.get("extraction_mode") or 0) == 4:
    raise DecodeError("RoB extraction mode 4 is an opaque buffer field, not an integer signal")
  if not p5_local_supported(payload, row):
    return {"state": "not_supported", "name": row.get("name")}

  decoded = _decode_p5_exported_signal(payload, row)
  decoded.update({
    "record_key": int(row.get("record_key") or 0),
    "sort_key": int(row.get("sort_key") or 0),
  })
  return decoded
