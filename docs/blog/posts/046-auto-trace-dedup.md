---
title: "🛡 Auto-Trace Deduplication: Stopping the Issue Flood"
date: 2026-09-05T01:02:34Z
draft: false
tags: [governance, automation, sentinel, reviewer]
categories: [governance]
summary: "The AI reviewer once created 52 identical auto-trace issues for a single PR. Rule 90 makes that impossible: dedup search, live PR body reads, and workflow concurrency."
artifacts:
  - {type: spec, path: openspec/changes/steering-rule-90-auto-trace-dedup}
  - {type: issue, id: "1057"}
  - {type: pr, id: "1058"}
related:
  - 015-sentinel-security-audit
  - 024-bolt-performance-engineering
---
# 🛡 Auto-Trace Deduplication

Automated governance is only as good as its ability to fail *once*. When the AI reviewer
hit a PR whose body lacked an issue link, it was supposed to create one tracking issue
(Rule 11) and move on. Instead, it created **52**.

## The Incident

PR #996 ("Path Traversal Bypass via Sanitization") triggered the reviewer on every push
(`synchronize` event). Each run saw a PR body without a `Resolves` link, decided "Rule 11
violation", and created a new `[Auto-Trace]` issue. Three defects compounded:

1. **No concurrency control** — parallel workflow runs for the same PR raced; each created
   its own issue.
2. **No dedup search** — `create_missing_issue` never asked whether a tracking issue for
   this PR already existed.
3. **Stale event payload** — the issue-link check read the PR body from the webhook event,
   which lags behind `gh pr edit`. Even after a run appended `Resolves <url>`, the next
   `synchronize` still saw the old body.

Result: issues #997–#1054, 52 identical auto-trace entries cluttering the tracker.

## The Fix

### 1. Search before creating

`ai_reviewer.py` now has `find_existing_auto_trace(pr_number, pr_title)` — it searches
OPEN auto-trace issues matching `[Auto-Trace] <PR title>` (or a body referencing
`for PR #<n>`) and returns the canonical number. `create_missing_issue` reuses it and
links the PR body with `Resolves #<canonical>`, creating a new issue **only** when none
exists.

### 2. Live PR body, not event payload

`get_current_pr_body(pr_number)` fetches the current body from the GitHub API. The
`has_issue_link` check now sees links added by previous runs, so a subsequent
`synchronize` short-circuits.

### 3. Serialize runs

`gemini_reviewer.yml` gained a `concurrency` block (`cancel-in-progress: true`, keyed per
PR): a new push cancels the in-progress review, so only the latest commit is evaluated.

## Rule 90

All three behaviors are codified as **Rule 90 (Auto-Trace Issue Deduplication —
Mandatory)** in `openspec/config.yaml`:

- Never create a second auto-trace issue for a PR that already has one — reuse the canonical.
- Use the current PR body from the API, not the stale event payload.
- Rely on workflow `concurrency` to serialize parallel pushes.

## The Cleanup

The 52 duplicates were closed as duplicates of canonical #997 (Rule 20), and PR #996 was
linked `Resolves #997`. Only four legitimate auto-trace trackers remain open — each for a
distinct PR. Verification: `find_existing_auto_trace` against PR #996 returns an existing
issue number, proving the dedup would have prevented the flood.

Governance tooling that creates noise is worse than no tooling. Rule 90 keeps the reviewer
honest, silent, and *idempotent* — the automation posture in
[docs/STANDARDS.md](../../STANDARDS.md).

## Related

Sentinel posture: [015](015-sentinel-security-audit.md). Performance discipline:
[024](024-bolt-performance-engineering.md).
