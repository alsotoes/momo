#!/usr/bin/env python3
"""Unit tests for ai_workflow_check.py.

The checker reads module-level path constants (MANIFEST_PATH, OPENCODE_PROJECT,
CONFIG_YAML, ...), so setUp patches every one of them to the test tmpdir —
patching only checker.ROOT would leave the checker reading the real repo files.
"""

import json
import sys
import tempfile
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).parent))
import ai_workflow_check as checker


TEST_MANIFEST = """version: 1
harness:
  ai_reviewer:
    script: .github/scripts/ai_reviewer.py
    workflow: .github/workflows/gemini_reviewer.yml
    role: merge-gate
    rules: [70, 80]
plugins:
  - package: "@dietrichgebert/ponytail"
    pinned: "5.1.0"
    scope: project
    default_mode: "lite"
    allowed: [diff-review, minimalism-guidance]
    forbidden: [ultra-mode, skipping-openspec-ceremony, replacing-lint-gate]
    rules: [37, 73, 74]
  - package: "context7"
    scope: project
    requires_env: "CONTEXT7_API_KEY"
    rules: [73]
  - package: "opencode-plugin-mempalace"
    scope: global
    rules: [89]
invariants:
  - project opencode.json plugins declared in manifest
"""

TEST_CONFIG = """context: |
  # Project Context
  37. **Panic-Safe Resource Releasing**: recover and release resources.
  70. **AI Reviewer Gate**: reviewer must approve feature PRs.
  73. **Spec-First Implementation Mandate**: features need an OpenSpec change.
  74. **Seam-Over-Plugins**: prefer compile-time seams.
  80. **Jules PR Takeover**: rebuild rogue Jules PRs cleanly.
  89. **MemPalace Knowledge Grounding**: ground via mempalace search.
"""

TEST_OPENCODE_PROJECT = {
    "mcp": {"context7": {"command": "npx", "args": ["@context7/mcp-server"]}},
    "plugins": ["@dietrichgebert/ponytail@5.1.0"],
}

TEST_OPENCODE_GLOBAL = {"mcp": {"mempalace": {}}}


class TestAiWorkflowCheck(unittest.TestCase):
    def setUp(self):
        self.tmpdir = Path(tempfile.mkdtemp())
        (self.tmpdir / "openspec").mkdir(exist_ok=True)
        (self.tmpdir / ".github").mkdir(exist_ok=True)
        (self.tmpdir / ".github" / "ecc-tools.yml").write_text("analyzer: {}\n")
        (self.tmpdir / "mempalace.yaml").write_text("wing: test\n")
        (self.tmpdir / "openspec" / "config.yaml").write_text(TEST_CONFIG)
        (self.tmpdir / "openspec" / "ai-workflow.yaml").write_text(TEST_MANIFEST)
        (self.tmpdir / "opencode.json").write_text(json.dumps(TEST_OPENCODE_PROJECT))

        # Patch every module-level constant the checker reads.
        self._saved = {}
        for name, value in [
            ("MANIFEST_PATH", self.tmpdir / "openspec" / "ai-workflow.yaml"),
            ("OPENCODE_PROJECT", self.tmpdir / "opencode.json"),
            ("OPENCODE_GLOBAL", self.tmpdir / "global-opencode.json"),  # absent
            ("CONFIG_YAML", self.tmpdir / "openspec" / "config.yaml"),
            ("ECC_TOOLS_YML", self.tmpdir / ".github" / "ecc-tools.yml"),
            ("MEMPALACE_YAML", self.tmpdir / "mempalace.yaml"),
        ]:
            self._saved[name] = getattr(checker, name)
            setattr(checker, name, value)

    def tearDown(self):
        for name, value in self._saved.items():
            setattr(checker, name, value)
        import shutil

        shutil.rmtree(self.tmpdir, ignore_errors=True)

    def test_valid_manifest_passes(self):
        errors, warnings = checker.check_plugins()
        self.assertEqual(errors, [])
        self.assertEqual(warnings, [])

    def test_unpinned_ponytail_fails(self):
        checker.MANIFEST_PATH.write_text(
            TEST_MANIFEST.replace('pinned: "5.1.0"', 'pinned: "@latest"')
        )
        errors, _ = checker.check_plugins()
        self.assertTrue(any("@latest" in e for e in errors), errors)

    def test_ultra_mode_must_be_forbidden(self):
        checker.MANIFEST_PATH.write_text(
            TEST_MANIFEST.replace(
                "forbidden: [ultra-mode, skipping-openspec-ceremony, replacing-lint-gate]",
                "forbidden: [skipping-openspec-ceremony, replacing-lint-gate]",
            )
        )
        errors, _ = checker.check_plugins()
        self.assertTrue(any("ultra" in e and "forbidden" in e for e in errors), errors)

    def test_ultra_mode_must_not_be_allowed(self):
        checker.MANIFEST_PATH.write_text(
            TEST_MANIFEST.replace(
                "allowed: [diff-review, minimalism-guidance]",
                "allowed: [diff-review, minimalism-guidance, ultra-mode]",
            )
        )
        errors, _ = checker.check_plugins()
        self.assertTrue(any("ultra" in e and "allowed" in e for e in errors), errors)

    def test_default_mode_must_be_lite(self):
        checker.MANIFEST_PATH.write_text(
            TEST_MANIFEST.replace('default_mode: "lite"', 'default_mode: "ultra"')
        )
        errors, _ = checker.check_plugins()
        self.assertTrue(any("default_mode" in e and "lite" in e for e in errors), errors)

    def test_undeclared_project_plugin_flagged(self):
        project = json.loads(checker.OPENCODE_PROJECT.read_text())
        project["plugins"].append("some-undeclared-plugin@1.0.0")
        checker.OPENCODE_PROJECT.write_text(json.dumps(project))
        errors, _ = checker.check_plugins()
        self.assertTrue(any("undeclared-plugin" in e for e in errors), errors)

    def test_unknown_rule_reference_warns(self):
        checker.MANIFEST_PATH.write_text(
            TEST_MANIFEST.replace("rules: [37, 73, 74]", "rules: [37, 999]")
        )
        _, warnings = checker.check_plugins()
        self.assertTrue(any("#999" in w for w in warnings), warnings)

    def test_secret_in_project_config_flagged(self):
        checker.OPENCODE_PROJECT.write_text(
            json.dumps({"plugins": ["x"], "key": "sk-" + "a" * 40})
        )
        errors, _ = checker.check_plugins()
        self.assertTrue(any("secret" in e.lower() for e in errors), errors)


    def test_scoped_package_version_suffix_matches_bare_declaration(self):
        """A config entry 'pkg@x.y.z' must match a manifest declaration of the
        bare scoped package '@scope/pkg' (and vice versa) — no false 'not
        declared' errors for npm-scoped names."""
        checker.MANIFEST_PATH.write_text("""version: 1
plugins:
  - package: "@scope/pkg"
    pinned: "1.2.3"
    scope: project
    default_mode: "lite"
    allowed: [x]
    forbidden: [ultra-mode]
  - package: "@scope/other@2.0.0"
    pinned: "2.0.0"
    scope: project
""")
        checker.OPENCODE_PROJECT.write_text(
            json.dumps({"plugins": ["@scope/pkg@1.2.3", "@scope/other"]})
        )
        errors, _ = checker.check_plugins()
        undeclared = [e for e in errors if "not declared" in e]
        self.assertEqual(undeclared, [])

if __name__ == "__main__":
    unittest.main()

if __name__ == "__main__":
    unittest.main()
