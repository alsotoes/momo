#!/usr/bin/env python3
"""Golden evaluation suite for .github/scripts/ai_reviewer.py.

Evaluates deterministic gate logic, approval parser safety against simulated
mutant review outputs, and Sonar gate verdicts against a golden dataset of
historical Sentinel, Bolt, and Governance cases.

Run:
    python3 .github/scripts/eval_ai_reviewer.py --mode offline
"""

import argparse
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


GOLDEN_REVIEW_CASES = [
    {
        "id": "clean_approval",
        "review": "✅ All Project Steering Rules and architectural patterns are respected.",
        "expected_approved": True,
    },
    {
        "id": "jules_prefixed_approval",
        "review": "@google-labs-jules\n\n✅ All Project Steering Rules and architectural patterns are respected.",
        "expected_approved": True,
    },
    {
        "id": "sentinel_raw_recover_blocker",
        "review": "- 🚨 VIOLATION (Rule 21): Raw recover() used outside src/common/recover_helper.go.",
        "expected_approved": False,
    },
    {
        "id": "circuit_breaker_blocker",
        "review": "- 🛑 AI LOOP CIRCUIT BREAKER: @google-labs-jules has already made 3 attempts.",
        "expected_approved": False,
    },
    {
        "id": "approval_with_ecc_failure_advisory",
        "review": (
            "✅ All Project Steering Rules and architectural patterns are respected.\n\n"
            "---\n"
            "**ECC audit** (advisory) — Security Evidence: ❌ action required"
        ),
        "expected_approved": True,
    },
    {
        "id": "blocker_despite_approval_text",
        "review": (
            "✅ Good progress overall, however:\n"
            "- 🚨 VIOLATION (Rule 10): Returning raw errors.New instead of syscall.EINVAL."
        ),
        "expected_approved": False,
    },
]


class GoldenReviewerEvalSuite(unittest.TestCase):
    def test_golden_review_approval_matrix(self):
        for case in GOLDEN_REVIEW_CASES:
            with self.subTest(case_id=case["id"]):
                got = ai_reviewer.review_is_approved(case["review"])
                self.assertEqual(
                    got,
                    case["expected_approved"],
                    f"case {case['id']} approval mismatch",
                )

    def test_golden_gate_facts_contract(self):
        facts = ai_reviewer.build_gate_facts(
            spec_present=True,
            blog_present=False,
            no_blog_exempt=True,
            has_issue_link=True,
            is_enhancement=True,
        )
        expected_lines = [
            "OpenSpec change present: True",
            "Blog post present: False",
            "No-blog justification present: True",
            "GitHub issue link present: True",
            "Enhancement label: True",
        ]
        for line in expected_lines:
            self.assertIn(line, facts)

    def test_golden_sonar_gate_prevents_premature_merge(self):
        # Issue #1255: Never merge before Sonar check registers unless timeout reached.
        action, _ = ai_reviewer.sonar_wait_verdict(
            pending=[], failed=[], settled=0, seen_sonar=False, timed_out=False
        )
        self.assertEqual(action, "wait")

        # Failed Sonar check must block merge immediately.
        action, _ = ai_reviewer.sonar_wait_verdict(
            pending=[],
            failed=["SonarCloud Code Analysis"],
            settled=0,
            seen_sonar=True,
            timed_out=False,
        )
        self.assertEqual(action, "fail")

        # Settled Sonar check allows auto-merge.
        action, _ = ai_reviewer.sonar_wait_verdict(
            pending=[], failed=[], settled=1, seen_sonar=True, timed_out=False
        )
        self.assertEqual(action, "ok")

    def test_golden_no_blog_exemption_detection(self):
        completed = mock.Mock()
        completed.stdout = "docs/adr/0095-internal.md\ndocs/adr/0095-internal.no-blog.md\n"
        with mock.patch.object(ai_reviewer.subprocess, "run", return_value=completed):
            self.assertTrue(ai_reviewer.has_no_blog_justification("Resolves #1378"))


def main():
    parser = argparse.ArgumentParser(description="Evaluate ai_reviewer.py against golden cases")
    parser.add_argument(
        "--mode",
        choices=["offline"],
        default="offline",
        help="Evaluation mode (offline runs deterministic golden suite without API calls)",
    )
    _, remaining = parser.parse_known_args()
    suite = unittest.defaultTestLoader.loadTestsFromTestCase(GoldenReviewerEvalSuite)
    runner = unittest.TextTestRunner(verbosity=2)
    result = runner.run(suite)
    sys.exit(0 if result.wasSuccessful() else 1)


if __name__ == "__main__":
    main()
