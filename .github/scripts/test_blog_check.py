#!/usr/bin/env python3
"""Unit tests for the blog readability checks (issue #1116).

Run: python3 .github/scripts/test_blog_check.py
"""
import importlib.util
import sys
import unittest
from pathlib import Path

_SPEC = importlib.util.spec_from_file_location(
    "blog_check", Path(__file__).resolve().parent / "blog_check.py"
)
assert _SPEC is not None and _SPEC.loader is not None
blog_check = importlib.util.module_from_spec(_SPEC)
sys.modules["blog_check"] = blog_check
_SPEC.loader.exec_module(blog_check)


def _problems(body: str) -> list[str]:
    text = f"---\ntitle: x\n---\n{body}"
    out: list[str] = []
    blog_check.check_readability(
        blog_check.ROOT / "docs/blog/posts/999-test.md", text, out
    )
    return out


class ReadabilityTest(unittest.TestCase):
    def test_compliant_post_passes(self):
        body = (
            "We hit a wall during a load test.\n\n"
            "## The Real Problem\n\n"
            "The store re-hashed every read.\n\n"
            "## References / Dig deeper\n\n"
            "- `src/storage/storage.go`\n"
            "- openspec/changes/x/\n"
        )
        self.assertEqual(_problems(body), [])

    def test_missing_references_flagged(self):
        body = "We hit a wall.\n\n## The Real Problem\n\nThe store re-hashed every read.\n"
        self.assertTrue(any("References" in p for p in _problems(body)))

    def test_repo_path_in_prose_flagged(self):
        body = (
            "We changed src/storage/storage.go to fix it.\n\n"
            "## References\n\n- spec\n"
        )
        self.assertTrue(any("repository path" in p for p in _problems(body)))

    def test_standards_link_allowed(self):
        body = (
            "We hit a wall.\n\nSee docs/STANDARDS.md for the mindsets.\n\n"
            "## References\n\n- spec\n"
        )
        self.assertFalse(any("docs/STANDARDS.md" in p for p in _problems(body)))

    def test_latex_flagged(self):
        body = (
            "We hit a wall.\n\nIt uses $0\\text{ bytes}$ of state.\n\n"
            "## References\n\n- spec\n"
        )
        self.assertTrue(any("LaTeX" in p for p in _problems(body)))

    def test_path_in_fenced_code_allowed(self):
        body = (
            "We hit a wall.\n\n```go\n// src/storage/storage.go\nfunc f() {}\n```\n\n"
            "## References\n\n- spec\n"
        )
        self.assertFalse(any("repository path" in p for p in _problems(body)))

    def test_changelog_opening_flagged(self):
        body = "- shipped X\n- shipped Y\n\n## References\n\n- spec\n"
        self.assertTrue(any("narrative opening" in p for p in _problems(body)))


if __name__ == "__main__":
    unittest.main(verbosity=2)
