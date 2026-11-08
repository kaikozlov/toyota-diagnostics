import os
import unittest
from unittest import mock

from toyota_diag import monitor, term


def styled_env():
  return mock.patch.dict(os.environ, {"CLICOLOR_FORCE": "1", "COLORTERM": "truecolor", "TERM": "xterm", "NO_COLOR": ""})


def plain_env():
  return mock.patch.dict(os.environ, {"NO_COLOR": "1", "TERM": "xterm-foo"})


class Term(unittest.TestCase):
  def test_no_color_and_dumb_disable_styling(self):
    for env in ({"NO_COLOR": "1"}, {"TERM": "dumb"}):
      with mock.patch.dict(os.environ, env):
        self.assertEqual(term.dim("x"), "x")
        self.assertEqual(term.red("x"), "x")

  def test_forced_color_emits_truecolor(self):
    with styled_env():
      self.assertEqual(term.red("x"), "\x1b[38;2;242;139;130mx\x1b[0m")

  def test_light_theme_swaps_palette(self):
    with styled_env(), mock.patch.dict(os.environ, {"TOYOTA_THEME": "light"}):
      self.assertEqual(term.accent("x"), "\x1b[38;2;11;122;90mx\x1b[0m")

  def test_256_color_degradation_without_truecolor(self):
    with mock.patch.dict(os.environ, {"CLICOLOR_FORCE": "1", "TERM": "xterm-256color", "COLORTERM": "", "NO_COLOR": ""}):
      styled = term.red("x")
      self.assertIn("\x1b[38;5;", styled)
      self.assertNotIn("38;2;", styled)

  def test_width_strips_escapes(self):
    with styled_env():
      self.assertEqual(term.width(term.red("abcd")), 4)

  def test_title_right_aligns_meta(self):
    with plain_env(), mock.patch("shutil.get_terminal_size") as size:
      size.return_value = os.terminal_size((80, 24))
      line = term.title(term.bold("Engine"), "sample 3 · +1.234s")
      self.assertEqual(len(line), 80)
      self.assertTrue(line.endswith("sample 3 · +1.234s"))

  def test_title_wraps_meta_when_narrow(self):
    with plain_env(), mock.patch("shutil.get_terminal_size") as size:
      size.return_value = os.terminal_size((40, 24))

      lines = term.title("a-very-long-engine-name-here-today-yes", "meta").splitlines()
      self.assertEqual(lines, ["a-very-long-engine-name-here-today-yes", "meta"])


  def test_kv_shape(self):
    with plain_env():
      self.assertEqual(term.kv("Backend", "panda"), "  Backend       panda")


class MonitorTable(unittest.TestCase):
  @staticmethod
  def values():
    return [
      {"ecu": {"key": "engine"}, "did": 4098, "signals": [
        {"name": "BATT Voltage", "value": 12.4, "raw": 6348, "unit": "V"},
        {"name": "Drive Mode Status", "value": "Sport", "pattern": "Sport"}]},
      {"ecu": {"key": "engine"}, "did": 4102, "signals": [{"name": "Bad", "value": None, "error": "timeout"}]},
    ]

  def test_columns_align_between_header_and_rows(self):
    with plain_env():
      text, _ = monitor.render_table("Engine", 1, 0.5, self.values())
      lines = text.splitlines()
      header = lines[1]
      for row, did, signal in zip(lines[2:], ("0x1002", "0x1002", "0x1006"),
                                  ("BATT Voltage", "Drive Mode Status", "Bad")):
        self.assertEqual(row.index(did), header.index("DID"), f"DID column misaligned:\n{text}")
        self.assertEqual(row.index(signal), header.index("Signal"), f"Signal column misaligned:\n{text}")

  def test_plain_output_carries_no_escapes(self):
    with plain_env():
      text, _ = monitor.render_table("Engine", 1, 0.5, self.values())
      self.assertNotIn("\x1b[", text)
      self.assertIn("ERR: timeout", text)

  def test_styled_output_colors_did_and_error(self):
    with styled_env():
      text, _ = monitor.render_table("Engine", 1, 0.5, self.values())
      self.assertIn(term.blue("0x1002  "), text)
      self.assertIn(term.red("ERR: timeout".ljust(22)), text)

  def test_multi_ecu_adds_ecu_column(self):
    multi = [
      {"ecu": {"key": "engine"}, "did": 4098, "signals": [{"name": "BATT Voltage", "value": 12.4, "unit": "V"}]},
      {"ecu": {"key": "frc"}, "did": 5633, "signals": [{"name": "Steer", "value": 1, "unit": None}]},
    ]
    with plain_env():
      text, _ = monitor.render_table("engine + frc", 1, 0.1, multi)
      lines = text.splitlines()
      self.assertTrue(lines[1].startswith("  ECU "))
      self.assertTrue(lines[2].startswith("  engine "))
      self.assertTrue(lines[3].startswith("  frc "))

  def test_no_changes_is_quiet(self):
    with plain_env():
      text, state = monitor.render_table("frc", 2, 0.5, self.values())
      text, _ = monitor.render_table("frc", 3, 0.8, self.values(), changed=True, previous=state)
      self.assertIn("(no changes)", text)


if __name__ == "__main__":
  unittest.main()
