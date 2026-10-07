"""Unit tests for review_is_approved() in ai_reviewer.py (issue #1140).

Run: python3 .github/scripts/test_ai_reviewer_approval.py
"""

import importlib.util
import sys
import unittest
from pathlib import Path

_SPEC = importlib.util.spec_from_file_location(
    "ai_reviewer", Path(__file__).resolve().parent / "ai_reviewer.py"
)
assert _SPEC is not None and _SPEC.loader is not None
ai_reviewer = importlib.util.module_from_spec(_SPEC)
sys.modules["ai_reviewer"] = ai_reviewer
_SPEC.loader.exec_module(ai_reviewer)


class ReviewIsApprovedTest(unittest.TestCase):
    def test_plain_approval(self):
        self.assertTrue(ai_reviewer.review_is_approved(
            "✅ All Project Steering Rules and architectural patterns are respected."))

    def test_approval_prefixed_with_jules_mention(self):
        # The exact regression from #1136: a leading @-mention hid the marker.
        review = ("@google-labs-jules\n\n"
                  "✅ All Project Steering Rules and architectural patterns are respected.")
        self.assertTrue(ai_reviewer.review_is_approved(review))

    def test_approval_marker_in_middle(self):
        review = ("### Review\n\nEverything is sound.\n\n"
                  "✅ All Project Steering Rules and architectural patterns are respected.")
        self.assertTrue(ai_reviewer.review_is_approved(review))

    def test_blocker_is_never_approved(self):
        review = ("🚨 VIOLATION (Rule 11): missing issue link.\n"
                  "✅ All Project Steering Rules are respected.")
        self.assertFalse(ai_reviewer.review_is_approved(review))

    def test_marker_mention_in_prose_does_not_block(self):
        # Regression: a review that merely describes the blockers must still be
        # approvable (this exact case blocked auto-merge on PR #1141).
        review = ("The fix handles blockers (🚨/🛑/VIOLATION) correctly.\n\n"
                  "✅ All Project Steering Rules and architectural patterns are respected.")
        self.assertTrue(ai_reviewer.review_is_approved(review))

    def test_bulleted_blocker_is_not_approved(self):
        review = ("Findings:\n- 🚨 VIOLATION (Rule 73): missing OpenSpec change.\n\n"
                  "Action required.")
        self.assertFalse(ai_reviewer.review_is_approved(review))

    def test_circuit_breaker_is_not_approved(self):
        review = "🛑 AI LOOP CIRCUIT BREAKER: manual intervention required."
        self.assertFalse(ai_reviewer.review_is_approved(review))

    def test_findings_without_marker_not_approved(self):
        review = ("@google-labs-jules\n\n### Findings\n"
                  "1. Missing panic recovery.\n\nAction Required: fix it.")
        self.assertFalse(ai_reviewer.review_is_approved(review))

    def test_appended_ecc_block_does_not_approve(self):
        review = ("@google-labs-jules\n\n### Findings\nPlease fix.\n\n---\n"
                  "**ECC audit** (advisory) — Security Evidence: ✅ passed")
        self.assertFalse(ai_reviewer.review_is_approved(review))

    def test_empty_is_not_approved(self):
        self.assertFalse(ai_reviewer.review_is_approved(""))
        self.assertFalse(ai_reviewer.review_is_approved(None))


class RestHelpersTest(unittest.TestCase):
    """The label/assignee/body mutations must go through gh api (REST), not
    `gh pr edit` (GraphQL), which fails with a repo-only token (issue #1140)."""

    def setUp(self):
        self.calls = []
        orig_api = getattr(ai_reviewer, "_gh_api")
        orig_slug = getattr(ai_reviewer, "_repo_slug")

        def fake_api(method, path, payload=None):
            self.calls.append((method, path, payload))

            class _R:
                stdout = "{}"

            return _R()

        setattr(ai_reviewer, "_gh_api", fake_api)
        setattr(ai_reviewer, "_repo_slug", lambda: "alsotoes/momo")
        self.addCleanup(lambda: setattr(ai_reviewer, "_gh_api", orig_api))
        self.addCleanup(lambda: setattr(ai_reviewer, "_repo_slug", orig_slug))

    def test_add_pr_labels_dedups_and_sorts(self):
        ai_reviewer.add_pr_labels(5, ["jules", "bug", "bug"])
        self.assertEqual(
            self.calls,
            [("POST", "repos/alsotoes/momo/issues/5/labels",
              {"labels": ["bug", "jules"]})],
        )

    def test_assign_pr(self):
        ai_reviewer.assign_pr(5, "alsotoes")
        self.assertEqual(
            self.calls,
            [("POST", "repos/alsotoes/momo/issues/5/assignees",
              {"assignees": ["alsotoes"]})],
        )

    def test_set_pr_body(self):
        ai_reviewer.set_pr_body(5, "hello")
        self.assertEqual(
            self.calls,
            [("PATCH", "repos/alsotoes/momo/pulls/5", {"body": "hello"})],
        )

    def test_no_op_on_empty_inputs(self):
        ai_reviewer.add_pr_labels("", ["x"])
        ai_reviewer.add_pr_labels(5, [])
        ai_reviewer.assign_pr(5, "")
        ai_reviewer.set_pr_body("", "x")
        self.assertEqual(self.calls, [])


if __name__ == "__main__":
    unittest.main()
