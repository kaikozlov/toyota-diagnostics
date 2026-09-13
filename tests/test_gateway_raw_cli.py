"""Exercise gateway discovery bytes through the real CLI and ISO-TP transport."""
from __future__ import annotations

import unittest
from contextlib import redirect_stdout
from io import StringIO
from unittest import mock

from opendbc.car.structs import CarParams

from tests.support import FakePanda
from toyota_diag import cli, registry


class TestGatewayRawCli(unittest.TestCase):
  def test_raw_gateway_query_keeps_legacy_p5_and_negative_replies_distinct(self):
    cases = (
      ([], "3e", "7e"),
      (["00"], "3e00", "7e00"),
      ([], "3e", "7f3e13"),
    )
    for suffix, request_hex, reply_hex in cases:
      with self.subTest(request=request_hex, reply=reply_hex):
        reply = bytes.fromhex(reply_hex)

        class ReplyOnSend(FakePanda):
          def __init__(self, response):
            super().__init__()
            self.response = response

          def can_send(self, address, data, bus, timeout=None):
            super().can_send(address, data, bus, timeout)
            correct = bytes([0x5F, len(self.response)]) + self.response
            self._batches.append([
              (address, bytes(data), bus + 128),
              (0x758, bytes.fromhex("0f027e0000000000"), bus),
              (0x758, bytes.fromhex("6d027e0000000000"), bus),
              (0x758, correct.ljust(8, b"\0"), bus),
            ])

        panda = ReplyOnSend(reply)
        output = StringIO()
        args = [
          "--registry", str(registry.LEGACY_CAMRY_REGISTRY),
          "--bus", "1", "--obd-multiplexing", "uds", "raw", "0x750", "0x3E", *suffix,
          "--sub-address", "0x5F", "--rx-address", "0x758", "--rx-sub-address", "0x5F",
        ]
        with mock.patch("toyota_diag.transport.pandad_running", return_value=False), \
             mock.patch("panda.Panda", return_value=panda), redirect_stdout(output):
          result = cli.main(args)
        self.assertEqual(result, 0)
        request = bytes.fromhex(request_hex)
        expected = (bytes([0x5F, len(request)]) + request).ljust(8, b"\0")
        self.assertEqual(panda.sent, [(0x750, expected, 1)])
        self.assertEqual(panda.safety, [(CarParams.SafetyModel.elm327, 0)])
        self.assertIn(f"request:  {request_hex}", output.getvalue())
        self.assertIn(f"response: {reply_hex}", output.getvalue())
        # The CLI returns a raw negative response; a successful transport
        # exchange is not a positive ECU response or a gateway-family verdict.


if __name__ == "__main__":
  unittest.main()
