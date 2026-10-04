"""Pin the secure profile's CLI argument order without requiring Docker.

The checked-in command deliberately uses a flat YAML flow sequence of scalars.
CI additionally starts the real profile to verify parsing, secrets and listeners.
"""
from pathlib import Path
import re
import shlex
import unittest


class SecureComposeCommandTests(unittest.TestCase):
    def test_options_precede_the_only_storage_endpoint(self):
        profile = Path(__file__).resolve().parents[1] / "docker-compose.secure.yml"
        matches = re.findall(r"^    command: \[(.*)\]$", profile.read_text(), re.M)
        self.assertEqual(len(matches), 1, "expected one explicit flat command sequence")
        lexer = shlex.shlex(matches[0], posix=True)
        lexer.whitespace = ","
        lexer.whitespace_split = True
        args = [token.strip() for token in lexer]
        self.assertEqual(args, ["server", "--console-address", ":9001", "/data"],
                         "the CLI stops parsing flags at the first storage endpoint")


if __name__ == "__main__":
    unittest.main()
