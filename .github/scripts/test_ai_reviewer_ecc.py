"""Unit tests for the ECC Tools audit parser in ai_reviewer.py (issue #1134).

Run: python3 .github/scripts/test_ai_reviewer_ecc.py
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


def _comment(body, created, login="ecc-tools"):
    return {"author": {"login": login}, "body": body, "createdAt": created}


def _audit(dimension, sha, phrase, status):
    return (
        f"<!-- ecc-tools:pr-audit:ECC Tools / {dimension}:{sha} -->\n"
        f"## ECC Tools / {dimension}\n\n"
        f"Commit: `{sha}`\n\n"
        f"**{phrase}** ({status})\n\n"
        "Check publication was denied or unavailable."
    )


class EccAuditSummaryTest(unittest.TestCase):
    def test_no_comments_returns_empty(self):
        self.assertEqual(ai_reviewer.parse_ecc_audit_summary([]), "")

    def test_ignores_non_ecc_authors(self):
        comments = [
            _comment(_audit("Security Evidence", "a", "x", "success"),
                     "2026-01-01T00:00:00Z", login="someone-else")
        ]
        self.assertEqual(ai_reviewer.parse_ecc_audit_summary(comments), "")

    def test_ignores_comments_without_marker(self):
        comments = [_comment("just a normal comment", "2026-01-01T00:00:00Z")]
        self.assertEqual(ai_reviewer.parse_ecc_audit_summary(comments), "")

    def test_status_mapping_and_fixed_order(self):
        comments = [
            _comment(_audit("Security Evidence", "sha1", "Security evidence gate passed", "success"), "2026-01-01T00:00:00Z"),
            _comment(_audit("PR Risk Taxonomy", "sha1", "PR taxonomy review recommended", "neutral"), "2026-01-01T00:00:01Z"),
            _comment(_audit("Reference Set Readiness", "sha1", "Reference set readiness gaps detected", "neutral"), "2026-01-01T00:00:02Z"),
            _comment(_audit("Hosted Promotion Readiness", "sha1", "Hosted promotion readiness passed", "success"), "2026-01-01T00:00:03Z"),
        ]
        out = ai_reviewer.parse_ecc_audit_summary(comments)
        self.assertIn("Security Evidence: ✅", out)
        self.assertIn("PR Risk Taxonomy: ⚠️", out)
        self.assertIn("Reference Set Readiness: ⚠️", out)
        self.assertIn("Hosted Promotion Readiness: ✅", out)
        self.assertLess(out.index("Security Evidence"), out.index("PR Risk Taxonomy"))
        self.assertLess(out.index("PR Risk Taxonomy"), out.index("Hosted Promotion Readiness"))

    def test_latest_verdict_per_dimension_wins(self):
        comments = [
            _comment(_audit("Security Evidence", "old", "Security evidence gate failed", "failure"), "2026-01-01T00:00:00Z"),
            _comment(_audit("Security Evidence", "new", "Security evidence gate passed", "success"), "2026-01-02T00:00:00Z"),
        ]
        out = ai_reviewer.parse_ecc_audit_summary(comments)
        self.assertIn("✅ Security evidence gate passed", out)
        self.assertNotIn("❌", out)

    def test_failure_status_maps_to_cross(self):
        comments = [
            _comment(_audit("Security Evidence", "s", "Security evidence gate failed", "failure"), "2026-01-01T00:00:00Z"),
        ]
        out = ai_reviewer.parse_ecc_audit_summary(comments)
        self.assertIn("❌ Security evidence gate failed", out)

    def test_missing_verdict_line_falls_back(self):
        body = "<!-- ecc-tools:pr-audit:ECC Tools / Security Evidence:abc123 -->\n## ECC Tools / Security Evidence\n\nno verdict here"
        comments = [_comment(body, "2026-01-01T00:00:00Z")]
        out = ai_reviewer.parse_ecc_audit_summary(comments)
        self.assertIn("Security Evidence:", out)
        self.assertIn("reported", out)


if __name__ == "__main__":
    unittest.main(verbosity=2)
