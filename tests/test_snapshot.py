import json
import tempfile
import unittest
from pathlib import Path

from toyota_diag import registry, snapshot


class TestHealthCheckSnapshots(unittest.TestCase):
  @staticmethod
  def document(*, mount="responding", dtc_state="positive", dtcs=(), identity="4142", captured="before"):
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
        "identity": None if identity is None else {"state": "positive", "data_hex": identity},
      }],
    }

  def test_compare_tracks_mount_dtc_and_identity_changes(self):
    before = self.document(dtcs=(("U013187", 0x08), ("C123456", 0x01)), identity="4142")
    after = self.document(
      mount="no_response", dtc_state="negative_response",
      dtcs=(("U013187", 0x0A), ("C999999", 0x02)), identity="4344", captured="after",
    )
    diff = snapshot.compare(before, after)
    self.assertEqual(diff["summary"], {
      "changed_ecus": 1, "mount_state_changes": 1, "dtc_changes": 1, "identity_changes": 1,
    })
    row = diff["changes"][0]
    self.assertEqual(row["mount"], {"before": "responding", "after": "no_response"})
    self.assertEqual(row["dtc_state"], {"before": "positive", "after": "negative_response"})
    self.assertEqual(row["identity"], {"before": "4142", "after": "4344"})
    self.assertEqual(row["dtcs"]["added"], [{"code": "C999999", "status": 0x02}])
    self.assertEqual(row["dtcs"]["removed"], [{"code": "C123456", "status": 0x01}])
    self.assertEqual(row["dtcs"]["status_changed"], [{"code": "U013187", "before": 0x08, "after": 0x0A}])
    rendered = snapshot.render_diff(diff)
    self.assertIn("mount: responding -> no_response", rendered)
    self.assertIn("+ DTC C999999", rendered)
    self.assertIn("~ DTC U013187", rendered)

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
