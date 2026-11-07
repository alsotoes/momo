"""Unit tests for the Sonar merge gate in ai_reviewer.py (issue #1241).

Run: python3 .github/scripts/test_ai_reviewer_sonar.py
"""

import importlib.util
import sys
import unittest
from pathlib import Path


def load_reviewer():
    spec = importlib.util.spec_from_file_location(
        "ai_reviewer", Path(__file__).with_name("ai_reviewer.py"))
    mod = importlib.util.module_from_spec(spec)
    sys.modules["ai_reviewer"] = mod
    spec.loader.exec_module(mod)
    return mod


reviewer = load_reviewer()


def check(name, status, conclusion):
    return {"name": name, "status": status, "conclusion": conclusion}


class ClassifySonarChecksTest(unittest.TestCase):
    def test_no_sonar_checks_is_not_pending(self):
        pending, failed, settled = reviewer.classify_sonar_checks(
            [check("build", "COMPLETED", "SUCCESS")])
        self.assertEqual((pending, failed, settled), ([], [], 0))

    def test_running_sonar_is_pending(self):
        pending, failed, _ = reviewer.classify_sonar_checks(
            [check("SonarCloud Code Analysis", "IN_PROGRESS", "")])
        self.assertEqual(pending, ["SonarCloud Code Analysis"])
        self.assertEqual(failed, [])

    def test_completed_without_conclusion_is_pending(self):
        pending, _, _ = reviewer.classify_sonar_checks(
            [check("SonarCloud", "COMPLETED", "")])
        self.assertEqual(pending, ["SonarCloud"])

    def test_failed_sonar_is_failed(self):
        pending, failed, _ = reviewer.classify_sonar_checks(
            [check("SonarCloud Code Analysis", "COMPLETED", "FAILURE")])
        self.assertEqual(pending, [])
        self.assertEqual(failed, ["SonarCloud Code Analysis"])

    def test_success_is_settled(self):
        pending, failed, settled = reviewer.classify_sonar_checks(
            [check("SonarCloud", "COMPLETED", "SUCCESS")])
        self.assertEqual((pending, failed, settled), ([], [], 1))

    def test_skipped_and_neutral_are_settled(self):
        pending, failed, settled = reviewer.classify_sonar_checks([
            check("SonarCloud", "COMPLETED", "SKIPPED"),
            check("SonarCloud Code Analysis", "COMPLETED", "NEUTRAL"),
        ])
        self.assertEqual((pending, failed, settled), ([], [], 2))

    def test_cancelled_counts_as_failed(self):
        _, failed, _ = reviewer.classify_sonar_checks(
            [check("SonarCloud", "COMPLETED", "CANCELLED")])
        self.assertEqual(failed, ["SonarCloud"])

    def test_case_insensitive_name_and_conclusion(self):
        pending, failed, _ = reviewer.classify_sonar_checks(
            [{"name": "sonarcloud code analysis", "status": "completed", "conclusion": "failure"}])
        self.assertEqual(pending, [])
        self.assertEqual(failed, ["sonarcloud code analysis"])

    def test_context_key_fallback(self):
        # Commit statuses use "context" instead of "name".
        pending, _, _ = reviewer.classify_sonar_checks(
            [{"context": "SonarCloud", "state": "PENDING"}])
        self.assertEqual(pending, ["SonarCloud"])

    def test_mixed_rollup(self):
        pending, failed, settled = reviewer.classify_sonar_checks([
            check("build", "COMPLETED", "SUCCESS"),
            check("SonarCloud", "IN_PROGRESS", ""),
            check("SonarCloud Code Analysis", "COMPLETED", "SUCCESS"),
        ])
        self.assertEqual(pending, ["SonarCloud"])
        self.assertEqual(failed, [])
        self.assertEqual(settled, 1)


class SonarWaitVerdictTest(unittest.TestCase):
    """The gate must wait for Sonar to register before declaring success."""

    def test_no_checks_yet_waits(self):
        action, _ = reviewer.sonar_wait_verdict([], [], 0, False, False)
        self.assertEqual(action, "wait")

    def test_no_checks_after_timeout_skips(self):
        action, reason = reviewer.sonar_wait_verdict([], [], 0, False, True)
        self.assertEqual(action, "ok")
        self.assertIn("no Sonar checks registered", reason)

    def test_registered_and_settled_is_ok(self):
        action, reason = reviewer.sonar_wait_verdict([], [], 1, True, False)
        self.assertEqual(action, "ok")
        self.assertIn("1 Sonar check(s) settled", reason)

    def test_registered_and_pending_waits(self):
        action, _ = reviewer.sonar_wait_verdict(["SonarCloud"], [], 0, True, False)
        self.assertEqual(action, "wait")

    def test_registered_and_pending_after_timeout_fails(self):
        action, reason = reviewer.sonar_wait_verdict(["SonarCloud"], [], 0, True, True)
        self.assertEqual(action, "fail")
        self.assertIn("still running", reason)

    def test_failure_always_fails(self):
        action, _ = reviewer.sonar_wait_verdict([], ["SonarCloud"], 0, True, False)
        self.assertEqual(action, "fail")


if __name__ == "__main__":
    unittest.main()
