"""Keep the declared toolchain, build images and generators in sync."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]

class DependencyVersionTests(unittest.TestCase):
    def setUp(self):
        self.mod = (ROOT / "go.mod").read_text()
        self.version = re.search(r"^go (\S+)$", self.mod, re.M)[1]

    def test_go_build_images_match_module_minimum(self):
        for name in ("Dockerfile", "Dockerfile.cicd", "Dockerfile.release", "Dockerfile.dev.browser"):
            with self.subTest(file=name):
                self.assertIn(self.version, (ROOT / name).read_text())

    def test_minimum_check_and_documentation_match(self):
        for name in ("buildscripts/checkdeps.sh", "mint/preinstall.sh", "README.md", "README_zh_CN.md"):
            with self.subTest(file=name):
                self.assertIn(self.version, (ROOT / name).read_text())

    def test_ci_toolchains_match_module_minimum(self):
        for path in (ROOT / ".github/workflows").glob("*.yml"):
            if path.name.startswith("dependency-"):
                continue
            for value in re.findall(r"(?:GO_VERSION|go-version): [\"']?([0-9][^\s\"']*)", path.read_text()):
                with self.subTest(file=path.name):
                    self.assertEqual(value, self.version)

    def test_generators_match_runtime_modules(self):
        make = (ROOT / "Makefile").read_text()
        for variable, module in (("MSGP_VERSION", "github.com/tinylib/msgp"), ("STRINGER_VERSION", "golang.org/x/tools")):
            expected = re.search(re.escape(module) + r" (v\S+)", self.mod)[1]
            self.assertIn(variable + " ?= " + expected, make)

    def test_browser_ci_uses_the_committed_lockfile(self):
        workflow = (ROOT / ".github/workflows/go.yml").read_text()
        self.assertIn("bun install --frozen-lockfile", workflow)
        self.assertIn("git diff --exit-code -- production", workflow)

if __name__ == "__main__":
    unittest.main()
