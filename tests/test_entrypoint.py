from __future__ import annotations

import os
from pathlib import Path
import tempfile
import unittest
from unittest import mock

from toyota_diag import entrypoint


class TestEntrypoint(unittest.TestCase):
  def make_checkout(self, root: Path) -> Path:
    (root / "toyota_diag").mkdir(parents=True)
    (root / "toyota_diag" / "__init__.py").write_text("")
    (root / "pyproject.toml").write_text("[project]\nname='toyota-diagnostics'\n")
    return root

  def make_openpilot(self, root: Path) -> Path:
    (root / "openpilot").mkdir(parents=True)
    (root / "openpilot" / "__init__.py").write_text("")
    return root

  def make_python(self, path: Path) -> Path:
    path.parent.mkdir(parents=True)
    path.write_text("")
    return path

  def test_explicit_openpilot_runtime_wins_over_project_venv(self):
    with tempfile.TemporaryDirectory() as td:
      tmp = Path(td)
      checkout = self.make_checkout(tmp / "toyota-diagnostics")
      self.make_python(checkout / ".venv" / "bin" / "python")
      openpilot = self.make_openpilot(tmp / "openpilot")
      openpilot_python = self.make_python(tmp / "runtime" / "bin" / "python")
      plan = entrypoint.runtime_plan(
        checkout,
        env={
          entrypoint.OPENPILOT_ROOT_ENV: os.fspath(openpilot),
          entrypoint.OPENPILOT_PYTHON_ENV: os.fspath(openpilot_python),
        },
        current_python="/usr/bin/python3",
        agnos_marker=tmp / "not-agnos",
      )
    self.assertEqual(plan, entrypoint.RuntimePlan("comma-openpilot", openpilot_python, (checkout, openpilot)))

  def test_project_venv_is_desktop_fallback(self):
    with tempfile.TemporaryDirectory() as td:
      tmp = Path(td)
      checkout = self.make_checkout(tmp / "toyota-diagnostics")
      venv_python = self.make_python(checkout / ".venv" / "bin" / "python")
      plan = entrypoint.runtime_plan(
        checkout, env={}, current_python="/usr/bin/python3", agnos_marker=tmp / "not-agnos",
      )
    self.assertEqual(plan, entrypoint.RuntimePlan("project-venv", venv_python, (checkout,)))

  def test_bootstrapped_runtime_does_not_reexec(self):
    with tempfile.TemporaryDirectory() as td:
      tmp = Path(td)
      checkout = self.make_checkout(tmp / "toyota-diagnostics")
      self.make_python(checkout / ".venv" / "bin" / "python")
      plan = entrypoint.runtime_plan(
        checkout,
        env={entrypoint.BOOTSTRAP_ENV: "comma-openpilot"},
        current_python="/usr/bin/python3",
        agnos_marker=tmp / "not-agnos",
      )
    self.assertIsNone(plan)

  def test_virtualenv_launchers_are_not_collapsed_by_symlink_resolution(self):
    self.assertFalse(entrypoint._same_executable("/usr/local/venv/bin/python", "/data/toyota-diagnostics/.venv/bin/python"))

  def test_exec_runtime_preserves_source_precedence_and_cli_arguments(self):
    plan = entrypoint.RuntimePlan(
      "comma-openpilot", Path("/usr/local/venv/bin/python"),
      (Path("/data/toyota-diagnostics"), Path("/data/openpilot")),
    )
    with mock.patch.dict(os.environ, {"PYTHONPATH": "/existing", "VIRTUAL_ENV": "/wrong"}, clear=False), \
         mock.patch("toyota_diag.entrypoint.os.execve", side_effect=RuntimeError("captured")) as execve:
      with self.assertRaisesRegex(RuntimeError, "captured"):
        entrypoint.exec_runtime(plan, ["dtc", "scan"])
    executable, command, env = execve.call_args.args
    self.assertEqual(executable, "/usr/local/venv/bin/python")
    self.assertEqual(command, ["/usr/local/venv/bin/python", "-m", "toyota_diag.cli", "dtc", "scan"])
    self.assertEqual(env[entrypoint.BOOTSTRAP_ENV], "comma-openpilot")
    self.assertEqual(env["VIRTUAL_ENV"], "/usr/local/venv")
    self.assertEqual(env["PYTHONPATH"].split(os.pathsep)[:3], ["/data/toyota-diagnostics", "/data/openpilot", "/existing"])


if __name__ == "__main__":
  unittest.main()
