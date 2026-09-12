import unittest

from toyota_diag import decode, registry


class TestDecode(unittest.TestCase):
  @classmethod
  def setUpClass(cls):
    cls.profile = registry.load_registry(registry.LEGACY_CAMRY_REGISTRY)

  def test_cross_byte_msb0_extraction(self):
    self.assertEqual(decode.extract_msb0(bytes.fromhex("a53c"), 4, 11), 0x53)

  def test_signed_division_truncates_toward_zero(self):
    self.assertEqual(
      decode.convert_p5_physical(0xFF, bit_width=8, signed=True, mul=5, div=2, offset=0),
      -2,
    )

  def test_decimal_rendering_is_exact(self):
    self.assertEqual(decode.format_p5_decimal(-15, 1), "-1.5")
    self.assertEqual(decode.format_p5_decimal(15, 3), "0.015")

  def test_eps_steering_angle_witness(self):
    _, signals = self.profile.resolve_did("eps", "0x1037")
    self.assertEqual(len(signals), 1)
    self.assertEqual(
      decode.decode_signal(bytes.fromhex("0001"), signals[0]),
      {"raw": 1, "converted_integer": 15, "value": "1.5", "pattern": None},
    )
    self.assertEqual(decode.format_decoded_signal(bytes.fromhex("0001"), signals[0]),
                     "Steering Angle: 1.5 deg (raw=0x0001)")

  def test_frc_pattern_witness(self):
    _, signals = self.profile.resolve_did("frc", "0x1601")
    condition = next(row for row in signals if row["name"] == "LTA Control Condition")
    self.assertEqual(
      decode.format_decoded_signal(bytes.fromhex("00010000"), condition),
      "LTA Control Condition: LTA Disabled (raw=0x01)",
    )


  def test_current_p5_rob_steering_angle_and_local_support(self):
    eps = self.profile.lookup_ecu("eps")
    eps_rob = self.profile.category(eps)["rob"]
    steering = next(row for row in eps_rob["signals"] if row["name"] == "Steering Angle")
    self.assertEqual(decode.decode_rob_signal(bytes.fromhex("0001"), steering), {
      "state": "decoded", "name": "Steering Angle", "record_key": 7, "sort_key": 9,
      "raw": 1, "converted_integer": 15, "value": "1.5", "pattern": None,
      "unit": "deg", "formatted": "1.5 deg",
    })

    engine = self.profile.lookup_ecu("engine")
    engine_rob = self.profile.category(engine)["rob"]
    shift_p = next(row for row in engine_rob["signals"] if row["name"] == "Shift SW Status (P Range)")
    self.assertEqual(shift_p["local_support_mode"], 1)
    self.assertEqual(decode.decode_rob_signal(bytes.fromhex("0101"), shift_p)["formatted"], "ON")
    self.assertEqual(decode.decode_rob_signal(bytes.fromhex("0001"), shift_p), {
      "state": "not_supported", "name": "Shift SW Status (P Range)",
    })

  def test_current_p5_rob_unmaterialized_conditions_fail_closed(self):
    row = {
      "name": "x", "bit_start": 0, "bit_end": 7, "local_support_mode": 0, "extraction_mode": 1,
      "support_condition_key": 1, "dynamic_lsb_possible": False, "signal_info": {},
    }
    with self.assertRaisesRegex(decode.DecodeError, "cross-DID"):
      decode.decode_rob_signal(b"\x00", row)
    row["support_condition_key"] = 0
    row["dynamic_lsb_possible"] = True
    with self.assertRaisesRegex(decode.DecodeError, "dynamic-LSB"):
      decode.decode_rob_signal(b"\x00", row)

  def test_unknown_decoder_and_short_payload_fail_closed(self):
    with self.assertRaisesRegex(decode.DecodeError, "unsupported decoder"):
      decode.decode_signal(b"\x00", {"decoder": "unknown"})
    _, signals = self.profile.resolve_did("eps", "0x1037")
    with self.assertRaisesRegex(decode.DecodeError, "exceed 1-byte DID payload"):
      decode.decode_signal(b"\x00", signals[0])


if __name__ == "__main__":
  unittest.main()
