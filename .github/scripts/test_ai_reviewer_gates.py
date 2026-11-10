"""Unit tests for the deterministic artifact gates in ai_reviewer.py.

Run: python3 .github/scripts/test_ai_reviewer_gates.py
"""

import importlib.util
import sys
import unittest
from pathlib import Path
from unittest import mock

_SPEC = importlib.util.spec_from_file_location(
    "ai_reviewer", Path(__file__).resolve().parent / "ai_reviewer.py"
)
assert _SPEC is not None and _SPEC.loader is not None
ai_reviewer = importlib.util.module_from_spec(_SPEC)
sys.modules["ai_reviewer"] = ai_reviewer
_SPEC.loader.exec_module(ai_reviewer)


class NoBlogJustificationTest(unittest.TestCase):
    def _with_files(self, files):
        completed = mock.Mock()
        completed.stdout = "\n".join(files)
        return mock.patch.object(ai_reviewer.subprocess, "run", return_value=completed)

    def test_body_mention_is_enough(self):
        with self._with_files([]):
            self.assertTrue(ai_reviewer.has_no_blog_justification("no-blog: internal tooling"))

    def test_body_without_mention_and_no_file(self):
        with self._with_files(["docs/adr/0092-x.md"]):
            self.assertFalse(ai_reviewer.has_no_blog_justification("plain body"))

    def test_numbered_adr_justification_file(self):
        with self._with_files(["docs/adr/0092-x.md", "docs/adr/0092-x.no-blog.md"]):
            self.assertTrue(ai_reviewer.has_no_blog_justification("plain body"))

    def test_git_failure_is_not_an_exemption(self):
        with mock.patch.object(ai_reviewer.subprocess, "run", side_effect=RuntimeError("no repo")):
            self.assertFalse(ai_reviewer.has_no_blog_justification("plain body"))


class GateFactsTest(unittest.TestCase):
    def test_renders_every_fact(self):
        out = ai_reviewer.build_gate_facts(True, False, True, True, True)
        self.assertIn("OpenSpec change present: True", out)
        self.assertIn("Blog post present: False", out)
        self.assertIn("No-blog justification present: True", out)
        self.assertIn("GitHub issue link present: True", out)
        self.assertIn("Enhancement label: True", out)


if __name__ == "__main__":
    unittest.main()
