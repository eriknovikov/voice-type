"""Run production native insertion logic against a fake platform, never the desktop."""

import os
from pathlib import Path
import shlex
import subprocess
import tempfile
import unittest


ROOT = Path(__file__).resolve().parent.parent


class NativeInsertionTests(unittest.TestCase):
    def test_native_control_flow(self):
        with tempfile.TemporaryDirectory(prefix="voice-type-insert-") as directory:
            binary = Path(directory) / "insert-test"
            compiler = shlex.split(os.environ.get("CC", "cc"))
            build = subprocess.run(compiler + ["-std=c11", "-Wall", "-Wextra", "-Werror", "-pedantic",
                                               str(ROOT / "scripts/fixtures/macos_insert_test.c"),
                                               "-o", str(binary)], capture_output=True, text=True)
            self.assertEqual(build.returncode, 0, build.stderr)
            result = subprocess.run([str(binary)], capture_output=True, text=True)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertIn("PASS native control-flow fixture", result.stdout)
            print(result.stdout, end="")


if __name__ == "__main__":
    unittest.main()
