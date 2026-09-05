# Change: Steering Rule 90 — Auto-Trace Issue Deduplication

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1057 (tracking: duplicate auto-trace bug)

## Why

The AI reviewer created **52 duplicate auto-trace issues** (#997–#1054) for a single PR
(#996, "Path Traversal Bypass via Sanitization") because the reviewer script could not
deduplicate. Three independent defects combined:

1. **No concurrency control** in `gemini_reviewer.yml` — multiple workflow runs for the
   same PR executed in parallel; each created its own auto-trace issue.
2. **No dedup search in `create_missing_issue`** — the reviewer never checked whether an
   auto-trace issue already existed for the PR before creating a new one.
3. **Stale event payload** — the `has_issue_link` check read the PR body from the webhook
   event payload, which lags behind a previous run's `gh pr edit`. Every `synchronize`
   event therefore saw "no issue link" and created another issue.

This polluted the tracker with 52 identical issues, breaking Rule 20 (autonomous
traceability — no duplicate tracking issues) at scale. The fix codifies the prevention as a
mandatory steering rule so the defect class cannot recur.

## What Changes

- **`ai_reviewer.py`**:
  - New `find_existing_auto_trace(pr_number, pr_title)` — searches OPEN auto-trace issues
    matching `[Auto-Trace] <PR title>` (or a body referencing `for PR #<n>`) and returns the
    canonical issue number.
  - New `get_current_pr_body(pr_number)` — fetches the live PR body from the GitHub API
    instead of the stale webhook payload.
  - `create_missing_issue` now deduplicates: reuses the existing canonical issue (linking
    the PR body with `Resolves #<canonical>`) and only creates a new issue when none exists.
  - `main()` uses `get_current_pr_body` for the `has_issue_link` check.
- **`gemini_reviewer.yml`**: added `concurrency` block (`cancel-in-progress: true`, keyed
  per PR) so parallel pushes are serialized and only the latest commit is reviewed.
- **`openspec/config.yaml`**: added **Rule 90 (Auto-Trace Issue Deduplication — Mandatory)**.
- **`docs/AI_FLYING_SOLO.md`**: Rule 90 reference, Step 1c auto-trace guidance, Rule 80
  prevention note, and an automated-duplicate pitfall entry.

## Non-Goals

- No change to the rule-driven behavior of the reviewer for non-traceability checks.
- No manual-consolidation automation for already-created duplicates (that is an ops action,
  Rule 80, not reviewer behavior).

## Impact

- **Affected Specs:** `specs/steering-rule-90-auto-trace-dedup/spec.md` (requirements below).
- **Behavior:** reviewer creates at most one auto-trace issue per PR; parallel runs
  serialized; stale-payload duplicate creation eliminated.
- **Incident**: #1057 — 52 duplicates closed as dups of canonical #997; PR #996 linked
  `Resolves #997`.