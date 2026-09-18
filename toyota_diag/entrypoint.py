"""Dependency-free launcher for the Toyota diagnostics CLI.

A comma already has the authoritative openpilot Python runtime, compiled messaging
stack, Panda package, and the exact opendbc checkout used by the running software.
Do not duplicate those dependencies in this repository's standalone virtualenv.
The launcher selects that runtime before importing :mod:`toyota_diag.cli`.
"""
from __future__ import annotations

from dataclasses import dataclass
import importlib.util
import os
from pathlib import Path
import sys
from typing import Mapping, Sequence

BOOTSTRAP_ENV = "TOYOTA_DIAGNOSTICS_RUNTIME"
SOURCE_ROOT_ENV = "TOYOTA_DIAGNOSTICS_ROOT"
OPENPILOT_ROOT_ENV = "TOYOTA_OPENPILOT_ROOT"
OPENPILOT_PYTHON_ENV = "TOYOTA_OPENPILOT_PYTHON"

DEFAULT_SOURCE_ROOT = Path("/data/toyota-diagnostics")
DEFAULT_OPENPILOT_ROOT = Path("/data/openpilot")
DEFAULT_OPENPILOT_PYTHON = Path("/usr/local/venv/bin/python")
AGNOS_MARKER = Path("/AGNOS")


@dataclass(frozen=True)
class RuntimePlan:
  kind: str
  python: Path
  pythonpath: tuple[Path, ...]


def _is_checkout(path: Path) -> bool:
  return (path / "pyproject.toml").is_file() and (path / "toyota_diag" / "__init__.py").is_file()


def source_root(env: Mapping[str, str] | None = None) -> Path | None:
  """Return the source checkout that should remain import-authoritative."""
  env = os.environ if env is None else env
  explicit = env.get(SOURCE_ROOT_ENV)
  if explicit:
    root = Path(explicit).expanduser().absolute()
    if not _is_checkout(root):
      raise SystemExit(f"{SOURCE_ROOT_ENV} does not point at a toyota-diagnostics checkout: {root}")
    return root

  module_root = Path(__file__).absolute().parent.parent
  if _is_checkout(module_root):
    return module_root
  if _is_checkout(DEFAULT_SOURCE_ROOT):
    return DEFAULT_SOURCE_ROOT
  return None


def _same_executable(left: str | Path, right: str | Path) -> bool:
  # Do not resolve symlinks: different virtualenv Python launchers commonly point
  # at the same base interpreter but intentionally carry different sys.prefix.
  return os.path.abspath(os.fspath(left)) == os.path.abspath(os.fspath(right))


def runtime_plan(
  root: Path | None,
  *,
  env: Mapping[str, str] | None = None,
  current_python: str | Path | None = None,
  agnos_marker: Path = AGNOS_MARKER,
  default_openpilot_root: Path = DEFAULT_OPENPILOT_ROOT,
  default_openpilot_python: Path = DEFAULT_OPENPILOT_PYTHON,
) -> RuntimePlan | None:
  """Select the host runtime without importing any third-party package."""
  env = os.environ if env is None else env
  current_python = sys.executable if current_python is None else current_python
  if env.get(BOOTSTRAP_ENV):
    return None

  explicit_openpilot = OPENPILOT_ROOT_ENV in env or OPENPILOT_PYTHON_ENV in env
  if agnos_marker.is_file() or explicit_openpilot:
    if root is None:
      raise SystemExit("comma/openpilot runtime found, but the toyota-diagnostics source checkout could not be located")
    openpilot_root = Path(env.get(OPENPILOT_ROOT_ENV, os.fspath(default_openpilot_root))).expanduser().absolute()
    python = Path(env.get(OPENPILOT_PYTHON_ENV, os.fspath(default_openpilot_python))).expanduser().absolute()
    if not (openpilot_root / "openpilot" / "__init__.py").is_file():
      raise SystemExit(f"openpilot checkout not found at {openpilot_root}; set {OPENPILOT_ROOT_ENV} to override")
    if not python.is_file():
      raise SystemExit(f"openpilot Python runtime not found at {python}; set {OPENPILOT_PYTHON_ENV} to override")
    if not _same_executable(current_python, python):
      return RuntimePlan("comma-openpilot", python, (root, openpilot_root))
    return None

  if root is not None:
    python = root / ".venv" / "bin" / "python"
    if python.is_file() and not _same_executable(current_python, python):
      return RuntimePlan("project-venv", python, (root,))
  return None


def _prepend_pythonpath(env: dict[str, str], paths: Sequence[Path]) -> None:
  values = [os.fspath(path) for path in paths]
  existing = env.get("PYTHONPATH")
  if existing:
    values.extend(item for item in existing.split(os.pathsep) if item and item not in values)
  env["PYTHONPATH"] = os.pathsep.join(values)


def exec_runtime(plan: RuntimePlan, argv: Sequence[str]) -> None:
  env = dict(os.environ)
  env[BOOTSTRAP_ENV] = plan.kind
  env["VIRTUAL_ENV"] = os.fspath(plan.python.parent.parent)
  env.pop("PYTHONHOME", None)
  _prepend_pythonpath(env, plan.pythonpath)
  command = [os.fspath(plan.python), "-m", "toyota_diag.cli", *argv]
  os.execve(command[0], command, env)


def main(argv: Sequence[str] | None = None) -> int:
  """Launch the CLI in the one appropriate runtime for this machine."""
  args = list(sys.argv[1:] if argv is None else argv)
  root = source_root()
  plan = runtime_plan(root)
  if plan is not None:
    exec_runtime(plan, args)
    raise AssertionError("os.execve returned")

  if importlib.util.find_spec("opendbc") is None:
    hint = "run `uv sync` first" if root is not None else "install toyota-diagnostics with its dependencies"
    raise SystemExit(f"Toyota diagnostics dependencies are unavailable; {hint}")

  from toyota_diag.cli import main as cli_main
  return int(cli_main(args))
