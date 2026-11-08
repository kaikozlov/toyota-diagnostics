"""Live-output presentation: one palette and shape system with the Go CLI.

Mirrors internal/terminal (theme.go, layout.go): same colors, same NO_COLOR /
CLICOLOR_FORCE / TOYO_THEME / COLORFGBG handling, same line shapes. Machine
surfaces (--json/--jsonl/--csv) never pass through here.
"""
from __future__ import annotations

import os
import re
import shutil
import sys

MAX_MEASURE = 112
LABEL_WIDTH = 14
GUTTER = "  "

_DARK = {"accent": (114, 216, 177), "dim": (125, 138, 149), "blue": (143, 176, 255), "red": (242, 139, 130)}
_LIGHT = {"accent": (11, 122, 90), "dim": (102, 114, 124), "blue": (44, 85, 201), "red": (179, 38, 30)}

_ANSI = re.compile(r"\x1b\[[0-9;]*m")


def _light_background() -> bool:
  theme = (os.environ.get("TOYO_THEME") or os.environ.get("TOYOTA_THEME") or "").lower()
  if theme == "light":
    return True
  if theme == "dark":
    return False
  parts = os.environ.get("COLORFGBG", "").split(";")
  if len(parts) >= 2 and parts[-1].isdigit():
    return parts[-1] in ("7", "15")
  return False


def _color_enabled() -> bool:
  if os.environ.get("NO_COLOR") or os.environ.get("TERM") == "dumb":
    return False
  force = os.environ.get("CLICOLOR_FORCE", "")
  if force and force != "0":
    return True
  return sys.stdout.isatty()


def _truecolor() -> bool:
  colorterm = os.environ.get("COLORTERM", "").lower()
  return colorterm in ("truecolor", "24bit") or "truecolor" in os.environ.get("TERM", "").lower()


def _to_256(r: int, g: int, b: int) -> int:
  steps = (0, 95, 135, 175, 215, 255)
  cube = tuple(min(range(6), key=lambda i: abs(steps[i] - c)) for c in (r, g, b))
  exact = 16 + 36 * cube[0] + 6 * cube[1] + cube[2]
  gray = round((r + g + b) / 3)
  ramp = min(range(24), key=lambda i: abs(i * 10 + 8 - gray)) + 232
  cube_dist = sum((steps[c] - v) ** 2 for c, v in zip(cube, (r, g, b)))
  ramp_dist = 3 * ((ramp - 232) * 10 + 8 - gray) ** 2
  return ramp if ramp_dist < cube_dist else exact


def _paint(text: str, rgb: tuple[int, int, int]) -> str:
  if not _color_enabled() or text == "":
    return text
  r, g, b = rgb
  if _truecolor():
    return f"\x1b[38;2;{r};{g};{b}m{text}\x1b[0m"
  return f"\x1b[38;5;{_to_256(r, g, b)}m{text}\x1b[0m"


def _palette() -> dict[str, tuple[int, int, int]]:
  return _LIGHT if _light_background() else _DARK


def bold(text: str) -> str:
  return f"\x1b[1m{text}\x1b[0m" if _color_enabled() and text else text


def accent(text: str) -> str:
  return _paint(text, _palette()["accent"])


def dim(text: str) -> str:
  return _paint(text, _palette()["dim"])


def blue(text: str) -> str:
  return _paint(text, _palette()["blue"])


def red(text: str) -> str:
  return _paint(text, _palette()["red"])


def strip(text: str) -> str:
  return _ANSI.sub("", text)


def width(text: str) -> int:
  return len(strip(text))


def measure() -> int:
  return max(40, min(shutil.get_terminal_size().columns, MAX_MEASURE))


def title(left: str, right: str = "") -> str:
  right_plain = strip(right).strip()
  gap = measure() - width(left) - len(right_plain)
  if not right_plain:
    return left
  if gap < 2:
    return left + "\n" + dim(right_plain)
  return left + " " * gap + dim(right_plain)


def kv(label: str, value: str) -> str:
  if value == "":
    return ""
  head = dim(label.ljust(LABEL_WIDTH)) if _color_enabled() else label.ljust(LABEL_WIDTH)
  return GUTTER + head + value


def plural(count: int, one: str, many: str) -> str:
  return f"1 {one}" if count == 1 else f"{count} {many}"
