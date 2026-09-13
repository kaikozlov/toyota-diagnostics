import unittest

from toyota_diag import customize, registry


class TestCustomize(unittest.TestCase):
  @classmethod
  def setUpClass(cls):
    cls.database = registry.ToyotaDatabase.load()
    cls.camry = cls.database.profile("NA", 12704, bus=0)
    cls.p6 = cls.database.profile("NA", 12165, bus=0)

  def test_modern_item_lookup_uses_selected_vehicle_target(self):
    p5 = customize.lookup_item(self.camry, 1, 200)
    self.assertEqual((p5["name"], p5["target_category_id"], p5["write_did"]),
                     ("Wireless Control Function", 449, 0x225A))
    p6 = customize.lookup_item(self.p6, 1, 210)
    self.assertEqual((p6["name"], p6["target_category_id"], p6["write_did"]),
                     ("Wireless Remote Control Setting", 6033, 0x225A))

  def test_resolve_target_requires_installed_matching_phase(self):
    row = customize.lookup_item(self.camry, 1, 200)
    target = customize.resolve_target(self.camry, row)
    self.assertEqual((target.ecu.key, target.ecu.endpoint, target.phase_type, target.write_did, target.family),
                     ("jbunity", (0x750, 0x40), 0x22, 0x225A, "p5"))
    with self.assertRaisesRegex(customize.CustomizeError, "not installed"):
      customize.resolve_target(self.camry, customize.lookup_item(self.p6, 1, 210))

  def test_merge_value_reproduces_current_msbo_geometry(self):
    item = {"write_bit_start": 0, "write_bit_end": 7, "merge_mode": 0}
    self.assertEqual(customize.merge_value(bytes.fromhex("aa55"), item, 1), bytes.fromhex("0155"))
    item = {"write_bit_start": 2, "write_bit_end": 3, "merge_mode": 0}
    self.assertEqual(customize.merge_value(bytes.fromhex("00"), item, 1), bytes.fromhex("10"))
    item = {"write_bit_start": 9, "write_bit_end": 9, "merge_mode": 1}
    self.assertEqual(customize.merge_value(bytes.fromhex("4000"), item, 1), bytes.fromhex("4040"))
    with self.assertRaisesRegex(customize.CustomizeError, "not supported"):
      customize.merge_value(bytes.fromhex("0000"), item, 1)

  def test_choice_resolution_uses_oem_text(self):
    row = customize.lookup_item(self.p6, 1, 210)
    self.assertEqual(customize.resolve_choice(row, "w/o stop lump"), (1, "w/o stop lump"))
    self.assertEqual(customize.resolve_choice(row, "2"), (2, "w/  stop lump"))
    with self.assertRaisesRegex(customize.CustomizeError, "not an OEM choice"):
      customize.resolve_choice(row, 9)


if __name__ == "__main__":
  unittest.main()
