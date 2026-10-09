"""Keep the declared toolchain, build images and generators in sync."""
from pathlib import Path
import re
import unittest

ROOT = Path(__file__).resolve().parents[1]

class DependencyVersionTests(unittest.TestCase):
    def setUp(self):
        self.mod = (ROOT / "go.mod").read_text(encoding="utf-8")
        self.version = re.search(r"^go (\S+)$", self.mod, re.M)[1]

    def test_go_build_images_match_module_minimum(self):
        for name in ("Dockerfile", "Dockerfile.cicd", "Dockerfile.release", "Dockerfile.dev.browser"):
            with self.subTest(file=name):
                self.assertIn(self.version, (ROOT / name).read_text(encoding="utf-8"))

    def test_minimum_check_and_documentation_match(self):
        for name in ("buildscripts/checkdeps.sh", "mint/preinstall.sh", "README.md", "README_zh_CN.md"):
            with self.subTest(file=name):
                self.assertIn(self.version, (ROOT / name).read_text(encoding="utf-8"))

    def test_generators_match_runtime_modules(self):
        make = (ROOT / "Makefile").read_text(encoding="utf-8")
        expected = re.search(r"github\.com/tinylib/msgp (v\S+)", self.mod)[1]
        self.assertIn("MSGP_VERSION ?= " + expected, make)

    def test_stringer_has_an_explicit_tool_version(self):
        # stringer is installed separately and has no runtime dependency in go.mod.
        make = (ROOT / "Makefile").read_text(encoding="utf-8")
        self.assertRegex(make, r"(?m)^STRINGER_VERSION \?= v\d+\.\d+\.\d+$")
        self.assertIn("go install golang.org/x/tools/cmd/stringer@$(STRINGER_VERSION)", make)

    def test_browser_ci_uses_the_committed_lockfile(self):
        workflow = (ROOT / ".github/workflows/go.yml").read_text(encoding="utf-8")
        self.assertIn("bun install --frozen-lockfile", workflow)
        self.assertIn("git diff --exit-code -- production", workflow)

    def test_mint_sdk_pin_matches_server(self):
        sdk = "github.com/soulteary/otterio-sdk/v7"
        server = re.search(re.escape(sdk) + r" (v\S+)", self.mod)[1]
        mint = (ROOT / "mint/run/core/minio-go/go.mod").read_text(encoding="utf-8")
        self.assertEqual(re.search(re.escape(sdk) + r" (v\S+)", mint)[1], server)


def validate_go_workflow(text):
    """Check the repository's block-style setup-go steps without extra packages.

    This is a deliberately strict policy check, not a general YAML parser.
    Unsupported/inline setup-go forms fail closed rather than passing vacuously.
    """
    lines = text.splitlines()
    active = "\n".join(line for line in lines if not line.lstrip().startswith("#"))
    if re.search(r"(?m)^\s*(?:GO_VERSION|go-version)\s*:", active):
        raise ValueError("Go version overrides are forbidden; use go-version-file")
    if "env.GO_VERSION" in active:
        raise ValueError("obsolete env.GO_VERSION reference")
    count = 0
    for start, line in enumerate(lines):
        match = re.match(r"^( *)- (.*)$", line)
        if not match:
            continue
        indent = len(match[1])
        end = start + 1
        while end < len(lines):
            following = lines[end]
            if following.strip() and not following.lstrip().startswith("#"):
                if len(following) - len(following.lstrip(" ")) <= indent:
                    break
            end += 1
        block = "\n".join([match[2]] + [s[indent + 2:] for s in lines[start + 1:end]])
        uses = re.search(r"(?m)^uses:\s*[\"']?(actions/setup-go@[^\s\"']+)", block)
        if not uses:
            continue
        count += 1
        # Require one explicit with mapping and one direct file input. Inputs
        # inherited through YAML aliases/merge keys are intentionally disallowed.
        if not re.search(r"(?m)^with:\s*$", block):
            raise ValueError("setup-go requires an explicit with mapping")
        settings = re.search(r"(?m)^with:[ \t]*\n((?:  .*\n?|[ \t]*\n)*)", block)[1]
        files = re.findall(r"(?m)^  go-version-file:[ \t]*(.+)$", settings)
        if len(files) != 1 or files[0].split(" #", 1)[0].strip().strip("\"'") != "go.mod":
            raise ValueError("setup-go must read go-version-file: go.mod")
        if re.search(r"(?m)^  (?:go-version|<<):", settings):
            raise ValueError("setup-go version overrides/merged inputs are forbidden")
    if count != active.count("actions/setup-go@"):
        raise ValueError("unsupported setup-go syntax; use block-style steps")
    return count


class GoWorkflowPolicyTests(unittest.TestCase):
    VALID = """jobs:
  test:
    steps:
      - uses: actions/checkout@v7.0.1
      - name: Set up Go
        uses: actions/setup-go@v7.0.0
        with:
          go-version-file: go.mod
          cache: true
"""

    def test_all_ci_toolchains_read_go_mod(self):
        paths = sorted((ROOT / ".github/workflows").glob("*.yml"))
        paths += sorted((ROOT / ".github/workflows").glob("*.yaml"))
        self.assertTrue(paths, "no workflows were checked")
        count = 0
        for path in paths:
            with self.subTest(file=path.name):
                count += validate_go_workflow(path.read_text(encoding="utf-8"))
        self.assertGreater(count, 0, "no setup-go steps were checked")

    def test_named_and_unnamed_steps(self):
        self.assertEqual(validate_go_workflow(self.VALID), 1)
        self.assertEqual(validate_go_workflow(self.VALID.replace(
            "- name: Set up Go\n        uses:", "- uses:")), 1)

    def test_quoted_path_and_comment(self):
        self.assertEqual(validate_go_workflow(self.VALID.replace(
            "go-version-file: go.mod", "go-version-file: 'go.mod' # source")), 1)

    def test_rejects_missing_input(self):
        with self.assertRaises(ValueError):
            validate_go_workflow(self.VALID.replace("          go-version-file: go.mod\n", ""))

    def test_rejects_wrong_path(self):
        with self.assertRaises(ValueError):
            validate_go_workflow(self.VALID.replace("go-version-file: go.mod", "go-version-file: go.sum"))

    def test_rejects_version_override(self):
        for value in ("'1.27.1'", "${{ matrix.go }}", "${{ env.GO_VERSION }}"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                validate_go_workflow(self.VALID + "          go-version: " + value + "\n")

    def test_rejects_inline_mapping(self):
        with self.assertRaises(ValueError):
            validate_go_workflow("- {uses: actions/setup-go@v7, with: {go-version-file: go.mod}}")

    def test_rejects_duplicate_input(self):
        with self.assertRaises(ValueError):
            validate_go_workflow(self.VALID + "          go-version-file: go.mod\n")

    def test_comments_are_not_steps(self):
        self.assertEqual(validate_go_workflow("# - uses: actions/setup-go@v7\n"), 0)

if __name__ == "__main__":
    unittest.main()
