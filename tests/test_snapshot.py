import json
import tempfile
import unittest
from pathlib import Path

from toyota_diag import registry, snapshot


class TestHealthCheckSnapshots(unittest.TestCase):
  @staticmethod
  def document(*, mount="responding", dtc_state="positive", dtcs=(), identity="4142", captured="before", ffd=(), rob=()):
    records = [
      {"code": code, "status": status, "status_bits": [], "fault_status": True, "descriptions": []}
      for code, status in dtcs
    ]
    return {
      "schema": snapshot.SCHEMA,
      "captured_at": captured,
      "vehicle_type": 12704,
      "ecus": [{
        "key": "eps", "name": "Power Steering", "category_id": 405,
        "address": 0x7A1, "sub_addr": None,
        "mount": {"live_state": mount},
        "dtc": {"state": dtc_state, "records": records, "fault_count": len(records)},
        "freeze_frames": {
          "state": "available", "positive_dtc_count": len(ffd), "record_count": len(ffd),
          "dtcs": [{
            "dtc": code, "state": "positive", "records": [{
              "record_number": record, "ffd_type": 1, "identifier_count": 1,
              "identifiers": [{"did": did, "length": len(bytes.fromhex(data)), "data_hex": data}],
            }],
          } for code, record, did, data in ffd],
        },
        "rob": {
          "state": "available", "behavior_code_count": len(rob), "unique_behavior_code_count": len(set(rob)),
          "groups": [{"subfunction": 0x11, "state": "positive", "behavior_codes": list(rob)}],
        },
        "identity": None if identity is None else {"state": "positive", "data_hex": identity},
      }],
    }

  def test_compare_tracks_mount_dtc_and_identity_changes(self):
    before = self.document(
      dtcs=(("U013187", 0x08), ("C123456", 0x01)), identity="4142",
      ffd=(("U013187", 1, 0x1234, "aabb"), ("C123456", 1, 0x2222, "01")),
      rob=(0x2818, 0x2845),
    )
    after = self.document(
      mount="no_response", dtc_state="negative_response",
      dtcs=(("U013187", 0x0A), ("C999999", 0x02)), identity="4344", captured="after",
      ffd=(("U013187", 1, 0x1234, "ccdd"), ("C999999", 1, 0x3333, "02")),
      rob=(0x2845, 0x5285),
    )
    diff = snapshot.compare(before, after)
    self.assertEqual(diff["summary"], {
      "changed_ecus": 1, "mount_state_changes": 1, "dtc_changes": 1,
      "freeze_frame_changes": 1, "rob_changes": 1, "identity_changes": 1,
    })
    row = diff["changes"][0]
    self.assertEqual(row["mount"], {"before": "responding", "after": "no_response"})
    self.assertEqual(row["dtc_state"], {"before": "positive", "after": "negative_response"})
    self.assertEqual(row["identity"], {"before": "4142", "after": "4344"})
    self.assertEqual(row["dtcs"]["added"], [{"code": "C999999", "status": 0x02}])
    self.assertEqual(row["dtcs"]["removed"], [{"code": "C123456", "status": 0x01}])
    self.assertEqual(row["dtcs"]["status_changed"], [{"code": "U013187", "before": 0x08, "after": 0x0A}])
    self.assertEqual(row["freeze_frames"]["added"], [
      {"dtc": "C999999", "record_number": 1, "did": 0x3333, "data_hex": "02"},
    ])
    self.assertEqual(row["freeze_frames"]["removed"], [
      {"dtc": "C123456", "record_number": 1, "did": 0x2222, "data_hex": "01"},
    ])
    self.assertEqual(row["freeze_frames"]["changed"], [
      {"dtc": "U013187", "record_number": 1, "did": 0x1234, "before": "aabb", "after": "ccdd"},
    ])
    self.assertEqual(row["rob"], {
      "added": [{"subfunction": 0x11, "behavior_code": 0x5285}],
      "removed": [{"subfunction": 0x11, "behavior_code": 0x2818}],
    })
    rendered = snapshot.render_diff(diff)
    self.assertIn("mount: responding -> no_response", rendered)
    self.assertIn("+ DTC C999999", rendered)
    self.assertIn("~ DTC U013187", rendered)
    self.assertIn("~ FFD U013187", rendered)
    self.assertIn("+ RoB sub=0x11 code=0x5285", rendered)

  def test_p5_snapshot_plan_requires_exported_exact_category_contract(self):
    profile = registry.load_database().profile("NA", 12704, bus=0)
    eps = profile.lookup_ecu("eps")
    smart = profile.lookup_ecu("smart")
    plan = snapshot._p5_snapshot_plan(profile, eps)
    self.assertIsNotNone(plan)
    self.assertEqual(plan["requests"][0]["send"], "1904000000ff")
    self.assertIsNone(snapshot._p5_snapshot_plan(profile, smart))
    rob = snapshot._rob_inventory_plan(profile, eps)
    self.assertIsNotNone(rob)
    self.assertEqual([(row["send"], row["check"]) for row in rob["requests"]], [("ab01", "eb01"), ("ab11", "eb11")])
    self.assertIsNone(snapshot._rob_inventory_plan(profile, smart))

  def test_save_load_round_trip_and_schema_rejection(self):
    document = self.document()
    with tempfile.TemporaryDirectory() as directory:
      path = snapshot.save(document, Path(directory) / "health.json")
      self.assertEqual(snapshot.load(path), document)
      bad = Path(directory) / "bad.json"
      bad.write_text(json.dumps({"schema": "something-else"}))
      with self.assertRaisesRegex(registry.RegistryError, snapshot.SCHEMA):
        snapshot.load(bad)


if __name__ == "__main__":
  unittest.main()
