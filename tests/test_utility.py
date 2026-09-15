import unittest

from toyota_diag import registry, utility
from toyota_diag.executor import ExecutorError, PlanNotExecutable, RoutineTestPlan
from toyota_diag.session import DiagnosticSession
from tests import support

ADDR = support.SYNTH_ECU_ADDRESS


def executable_utility(**overrides):
  row = {
    "id": 0x3001, "name": "Fuel Pressure Check", "kind": "routine", "execution": "executable",
    "service": 0x31, "positive_response": 0x71, "session_requirement": "none", "fixed_request": True,
    "routine_id": 0x2002, "start_static": "31012002", "stop_static": "31022002", "result_static": "31032002",
  }
  row.update(overrides)
  return row


def executable_direct_utility(**overrides):
  row = {
    "id": 0x3003, "name": "Valve Utility", "kind": "direct", "execution": "executable",
    "service": 0x2F, "positive_response": 0x6F, "session_requirement": "none",
    "did": 0x2801, "start_prefix": "2f280103", "stop_prefix": "2f280100", "runtime_length": 2,
  }
  row.update(overrides)
  return row


class TestBundledRegistry(unittest.TestCase):
  @classmethod
  def setUpClass(cls):
    cls.profile = registry.load_registry(registry.LEGACY_CAMRY_REGISTRY)

  def test_bundled_v4_exposes_generic_families_but_no_concrete_utilities(self):
    self.assertIsNotNone(self.profile.session_control)
    self.assertEqual(utility.list_utilities(self.profile), [])
    self.assertEqual(self.profile.utilities("frc"), [])
    families = utility.list_families(self.profile)
    self.assertEqual(len(families), 10)
    self.assertEqual(utility.plan_family(self.profile, "0xD4")["semantic_kind"], "single_routine_active_test")
    with self.assertRaises(registry.RegistryError):
      self.profile.lookup_utility("frc", "0x3001")


class TestCurrentSimpleOperationUtility(unittest.TestCase):
  @classmethod
  def setUpClass(cls):
    cls.profile = registry.ToyotaDatabase.load().profile("NA", 12704, bus=0)
    cls.ecu = cls.profile.lookup_ecu(372)

  def _script_support(self, scripted, *, advertise: bool = True):
    endpoint = self.ecu.address
    root = bytearray(32)
    root[0x11 // 8] |= 0x80 >> (0x11 % 8)
    members = bytearray(32)
    if advertise:
      members[(0x87 - 1) // 8] |= 0x80 >> ((0x87 - 1) % 8)
    scripted.routine[(endpoint, 1, 0x1001)] = bytes(root)
    scripted.routine[(endpoint, 1, 0x1100)] = bytes(members)

  def test_bundled_simple_operation_materializes_exact_routine_plan(self):
    plan = utility.plan_utility(self.profile, self.ecu, "Reset Memory")
    self.assertIsInstance(plan, RoutineTestPlan)
    self.assertTrue(plan.executable)
    self.assertFalse(plan.parameterized)
    self.assertEqual(plan.rid, 0x1187)
    self.assertEqual(plan.start_option_prefix, b"")
    self.assertEqual(plan.stop_option_prefix, b"")
    self.assertEqual(plan.status_option_prefix, b"")

  def test_bundled_p6_simple_operation_materializes_exact_routine_plan(self):
    profile = registry.ToyotaDatabase.load().profile("NA", 12165, bus=0)
    ecu = profile.lookup_ecu(6000)
    plan = utility.plan_utility(profile, ecu, "Switch Specification Information")
    self.assertIsInstance(plan, RoutineTestPlan)
    self.assertTrue(plan.executable)
    self.assertEqual(plan.rid, 0xDA03)
    self.assertEqual(plan.status_control, 3)

  def test_p6_simple_operation_checks_live_rid_support_before_mutation(self):
    profile = registry.ToyotaDatabase.load().profile("NA", 12165, bus=0)
    ecu = profile.lookup_ecu(6000)
    scripted = support.ScriptedUds()
    root = bytearray(32)
    selector_index = 0xDA
    root[selector_index // 8] |= 0x80 >> (selector_index % 8)
    members = bytearray(32)
    members[0x03 // 8] |= 0x80 >> (0x03 % 8)
    scripted.routine[(ecu.address, 1, 0xD100)] = bytes(root)
    scripted.routine[(ecu.address, 1, 0xD1DA)] = bytes(members)
    plan = utility.plan_utility(profile, ecu, "Switch Specification Information")
    with DiagnosticSession(profile, ecu, client_factory=scripted.factory) as session:
      result = utility.run_utility(
        session, plan, hold_s=0.001, execute=True, poll_interval_s=1.0, echo=lambda text: None)
    self.assertTrue(result.executed)
    routine_calls = [call for call in scripted.calls if call[1] == "routine"]
    self.assertEqual([call[3] for call in routine_calls], [0xD100, 0xD1DA, 0xDA03, 0xDA03])

  def test_simple_operation_dry_run_does_not_query_support(self):
    scripted = support.ScriptedUds()
    plan = utility.plan_utility(self.profile, self.ecu, "Reset Memory")
    with DiagnosticSession(self.profile, self.ecu, client_factory=scripted.factory) as session:
      result = utility.run_utility(session, plan, hold_s=0.001, execute=False, echo=lambda text: None)
    self.assertFalse(result.executed)
    self.assertEqual(scripted.calls, [])

  def test_simple_operation_checks_live_rid_support_before_mutation(self):
    scripted = support.ScriptedUds()
    self._script_support(scripted)
    plan = utility.plan_utility(self.profile, self.ecu, "Reset Memory")
    with DiagnosticSession(self.profile, self.ecu, client_factory=scripted.factory) as session:
      result = utility.run_utility(
        session, plan, hold_s=0.001, execute=True, poll_interval_s=1.0, echo=lambda text: None)
    self.assertTrue(result.executed)
    routine_calls = [call for call in scripted.calls if call[1] == "routine"]
    self.assertEqual([call[3] for call in routine_calls], [0x1001, 0x1100, 0x1187, 0x1187])
    self.assertEqual([call[2] for call in routine_calls[-2:]], [1, 2])

  def test_simple_operation_refuses_unadvertised_rid_before_mutation(self):
    scripted = support.ScriptedUds()
    self._script_support(scripted, advertise=False)
    plan = utility.plan_utility(self.profile, self.ecu, "Reset Memory")
    with (DiagnosticSession(self.profile, self.ecu, client_factory=scripted.factory) as session,
          self.assertRaisesRegex(ExecutorError, "does not advertise utility RID 0x1187")):
      utility.run_utility(
        session, plan, hold_s=0.001, execute=True, poll_interval_s=1.0, echo=lambda text: None)
    self.assertFalse(any(call[1] == "routine" and call[3] == 0x1187 for call in scripted.calls))


class TestUtilityBackend(unittest.TestCase):
  def setUp(self):
    self.scripted = support.ScriptedUds()
    support.guard_pass(self.scripted)
    self.scripted.routine.update({
      (ADDR, 1, 0x2002): b"\x00", (ADDR, 2, 0x2002): b"\x00", (ADDR, 3, 0x2002): b"\x02",
    })
    self.profile = support.load_profile(None, utilities=[
      executable_utility(),
      executable_utility(id=0x3002, name="Plan Only Utility", kind="direct", execution="plan_only",
                         service=0x2F, positive_response=0x6F, did=0x2801,
                         start_prefix="2f280103", stop_prefix="2f280100"),
      executable_direct_utility(),
    ])
    self.ecu = support.synthetic_ecu(self.profile)

  def test_list_utilities_reports_rows_per_ecu(self):
    listed = utility.list_utilities(self.profile)
    self.assertEqual([(spec.key, len(rows)) for spec, rows in listed], [(self.ecu.key, 3)])
    self.assertEqual(utility.list_utilities(self.profile, self.ecu)[0][1][0]["name"], "Fuel Pressure Check")

  def test_plan_utility_resolves_by_id_and_name(self):
    by_id = utility.plan_utility(self.profile, self.ecu, "0x3001")
    by_name = utility.plan_utility(self.profile, self.ecu, "Fuel Pressure Check")
    self.assertIsInstance(by_id, RoutineTestPlan)
    self.assertTrue(by_id.executable)
    self.assertEqual(by_id.rid, 0x2002)
    self.assertEqual(by_name.test_id, 0x3001)

  def test_run_utility_executes_executable_routine_without_session_transitions(self):
    plan = utility.plan_utility(self.profile, self.ecu, "0x3001")
    with DiagnosticSession(self.profile, self.ecu, client_factory=self.scripted.factory) as session:
      result = utility.run_utility(session, plan, hold_s=0.01, execute=True, echo=lambda text: None)
    self.assertTrue(result.executed)
    controls = [call[2] for call in self.scripted.calls if call[1] == "routine"]
    self.assertEqual((controls[0], controls[-1]), (1, 2))
    self.assertEqual([call for call in self.scripted.calls if call[1] == "session"], [])  # requirement "none"

  def test_run_utility_plan_only_without_ack_transmits_nothing(self):
    plan = utility.plan_utility(self.profile, self.ecu, "0x3001")
    with DiagnosticSession(self.profile, self.ecu, client_factory=self.scripted.factory) as session:
      result = utility.run_utility(session, plan, hold_s=0.01, echo=lambda text: None)
    self.assertFalse(result.executed)
    self.assertEqual(self.scripted.calls, [])

  def test_plan_only_utility_fails_closed_on_ack(self):
    plan = utility.plan_utility(self.profile, self.ecu, "0x3002")
    self.assertFalse(plan.executable)
    with DiagnosticSession(self.profile, self.ecu, client_factory=self.scripted.factory) as session:
      with self.assertRaises(PlanNotExecutable):
        utility.run_utility(session, plan, hold_s=0.01, execute=True, echo=lambda text: None)
    self.assertEqual(self.scripted.calls, [])

  def test_direct_utility_requires_explicit_runtime_bytes(self):
    plan = utility.plan_utility(self.profile, self.ecu, "0x3003")
    with DiagnosticSession(self.profile, self.ecu, client_factory=self.scripted.factory) as session:
      with self.assertRaises(ExecutorError):
        utility.run_utility(session, plan, hold_s=0.01, execute=True, echo=lambda text: None)
    self.assertEqual(self.scripted.calls, [])  # refused before any read or transmit

  def test_unknown_kind_query_fails_closed(self):
    with self.assertRaises(registry.RegistryError):
      utility.plan_utility(self.profile, self.ecu, "no such utility")


if __name__ == "__main__":
  unittest.main()
