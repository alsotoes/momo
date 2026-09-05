# 0051-steering-rule-90-auto-trace-dedup

## Status
Accepted

## Confidence
High

## Context
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

## Decision
- no duplicate auto-trace issues per PR: The AI reviewer SHALL NOT create a second auto-trace tracking issue for a PR that already has one.
- current PR body for issue-link detection: The reviewer SHALL determine whether the PR body carries an issue link from the CURRENT PR body fetched via the GitHub API, not from the (possibly stale) webhook event payload.
- workflow concurrency control: The reviewer workflow SHALL serialize runs per PR so parallel pushes cannot race. ## REMOVED Requirements None. ## Acceptance Criteria 1. `ai_reviewer.py` exposes `find_existing_auto_trace` and `get_current_pr_body`. 2. `create_missing_issue` reuses an existing canonical auto-trace issue (verified by the #1057 incident reproduction: search for PR #996 returns an existing issue number). 3. `gemini_reviewer.yml` declares `concurrency` with `cancel-in-progress: true` keyed by PR. 4. Rule 90 present...

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/steering-rule-90-auto-trace-dedup/
- Blog: docs/blog/posts/...md
