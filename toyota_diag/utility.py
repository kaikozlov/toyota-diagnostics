"""Techstream-style Utilities backend over the registry catalogs.

Concrete utilities are catalog rows (`catalogs.<id>.utilities`). Current type-77
Simple Operation rows carry exact P5/P6 D8/D9/DA RoutineControl geometry and a
Toyota live RID-support gate; after that gate is validated they reuse the generic
routine executor. Top-level recovered generic utility-family metadata
(`utilities.bindings`) remains discovery-only and is never silently promoted into
a concrete per-ECU operation.
"""
from __future__ import annotations

from collections.abc import Callable
from typing import Any

from toyota_diag import executor, registry, resolver
from toyota_diag.executor import ActiveTestResult, DirectTestPlan, RoutineTestPlan, TestPlan
from toyota_diag.registry import EcuSpec, Profile
from toyota_diag.session import DiagnosticSession

PLAN_ONLY_NOTE = "no utility request is sent without an explicit execute acknowledgement"


def list_families(profile: Profile) -> list[dict[str, Any]]:
  """Recovered generic utility/plugin families; metadata-only in registry v4."""
  return profile.utility_bindings()


def plan_family(profile: Profile, query: str) -> dict[str, Any]:
  """Resolve one generic utility family without claiming a concrete ECU operation."""
  return profile.lookup_utility_family(query)


def list_utilities(profile: Profile, ecu: EcuSpec | None = None) -> list[tuple[EcuSpec, list[dict[str, Any]]]]:
  """All utility rows per ECU, in profile order; ECUs without utilities are omitted."""
  targets = [ecu] if ecu is not None else list(profile.ecus)
  return [(spec, rows) for spec in targets if (rows := profile.utilities(spec))]


def _simple_operation_executor_row(row: dict[str, Any]) -> dict[str, Any]:
  """Adapt one exact type-77 Simple Operation row to the generic routine executor.

  The generated row stays `plan_only` because Toyota performs a live RID-support
  admission step before mutation. Once that exact gate is present, the D8/D9/DA
  request geometry is otherwise complete and can reuse the ordinary 0x31 backend.
  """
  if row.get("kind") != "simple_operation":
    return row
  normalized = dict(row)
  normalized.update(kind="routine", service=executor.ROUTINE_SERVICE, fixed_request=True)
  gate = row.get("support_gate")
  try:
    identifier = registry.parse_int(gate.get("identifier"), "Simple Operation support identifier") if isinstance(gate, dict) else -1
    rid = registry.parse_int(row.get("routine_id"), "Simple Operation RID")
  except registry.RegistryError:
    return normalized
  if (row.get("execution") == "plan_only" and isinstance(gate, dict)
      and gate.get("kind") == "rid" and gate.get("mode") in {"p5-standard", "p6-standard"}
      and identifier == rid):
    normalized["execution"] = executor.EXECUTION_EXECUTABLE
  return normalized


def _utility_row_for_plan(profile: Profile, plan: TestPlan) -> dict[str, Any] | None:
  matches = []
  for row in profile.utilities(plan.ecu):
    try:
      item = registry.parse_int(row.get("id"), "utility id")
    except registry.RegistryError:
      continue
    if item == plan.test_id:
      matches.append(row)
  if len(matches) > 1:
    raise executor.ExecutorError(f"utility plan id 0x{plan.test_id:04X} is ambiguous in the selected ECU catalog")
  return matches[0] if matches else None


def _enforce_support_gate(session: DiagnosticSession, plan: TestPlan, row: dict[str, Any]) -> None:
  gate = row.get("support_gate")
  if gate is None:
    return
  if not isinstance(gate, dict):
    raise executor.ExecutorError("malformed utility support-gate metadata")
  if plan.ecu.category_id is None:
    raise executor.ExecutorError("utility support gate requires a Toyota category id")
  mode = resolver.support_mode(session.profile, plan.ecu.category_id)
  if gate.get("mode") != mode:
    raise executor.ExecutorError(
      f"utility support gate declares {gate.get('mode')!r}, but Toyota category {plan.ecu.category_id} selects {mode!r}")
  try:
    identifier = registry.parse_int(gate.get("identifier"), "utility support identifier")
  except registry.RegistryError as e:
    raise executor.ExecutorError(f"malformed utility support identifier: {e}") from e
  if gate.get("kind") != "rid":
    raise executor.ExecutorError(f"unsupported utility support-gate kind {gate.get('kind')!r}")
  if not isinstance(plan, RoutineTestPlan) or identifier != plan.rid:
    raise executor.ExecutorError(
      f"utility support RID 0x{identifier:04X} does not match the resolved routine plan")
  if plan.session_requirement == executor.SESSION_REQUIREMENT_EXTENDED:
    session.enter_extended()
  try:
    supported = resolver.rid_support_resolver(session.profile, plan.ecu.category_id, session.client()).supports(identifier)
  except resolver.ResolverError as e:
    raise executor.ExecutorError(f"Toyota RID support check failed: {e}") from e
  if not supported:
    raise executor.ExecutorError(
      f"Toyota RID support inventory does not advertise utility RID 0x{identifier:04X}")


def plan_utility(profile: Profile, ecu: EcuSpec | str | int, query: str, *, kind: str | None = None) -> TestPlan:
  """Resolve one utility row (numeric id or name query) into an executor plan."""
  spec = ecu if isinstance(ecu, EcuSpec) else profile.lookup_ecu(ecu)
  row = profile.lookup_utility(spec, query, kind=kind)
  return executor.resolve_plan(spec, _simple_operation_executor_row(row))


def run_utility(session: DiagnosticSession, plan: TestPlan, *, hold_s: float, execute: bool = False,
                option_record: bytes | None = None, value_payload: bytes = b"", control_enable_mask: bytes = b"",
                poll_interval_s: float = 0.5, echo: Callable[[str], None] = print) -> ActiveTestResult:
  """Run a planned utility through the matching executor backend."""
  if execute:
    refusals = executor.runtime_refusals(session.profile, plan)
    if refusals:
      raise executor.PlanNotExecutable(plan, refusals)
    row = _utility_row_for_plan(session.profile, plan)
    if row is not None:
      _enforce_support_gate(session, plan, row)
  if isinstance(plan, RoutineTestPlan):
    return executor.run_routine_test(session, plan, hold_s=hold_s, option_record=option_record, execute=execute,
                                     poll_interval_s=poll_interval_s, echo=echo)
  if isinstance(plan, DirectTestPlan):
    return executor.run_direct_test(
      session, plan, hold_s=hold_s, value_payload=value_payload,
      start_control_enable_mask=b"", stop_control_enable_mask=control_enable_mask,
      execute=execute, echo=echo,
    )
  raise executor.ExecutorError(f"expected a routine or direct utility plan, got kind {plan.kind!r}")
