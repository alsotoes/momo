---
title: "🛡 Auto-Trace Deduplication: Stopping the Issue Flood"
date: 2026-09-05T01:02:34Z
draft: false
post_type: issue
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
  - 052-rule-93-duplicate-jules-prs
---
Automated governance is only as good as its ability to fail *once*. An **idempotent**
operation is one you can run repeatedly and get the same result — running it twice
does not create a second copy of anything. Our AI reviewer was supposed to be
idempotent. When it saw a pull request whose description lacked a link to a tracking
issue, it was meant to open exactly one tracking issue (an **auto-trace** issue,
created automatically to trace the change) and move on. Instead, it opened **52**.

## The incident

The trigger was a pull request titled "Path Traversal Bypass via Sanitization." The
reviewer runs on every `synchronize` event, which GitHub fires each time a new commit
is pushed. Every one of those runs looked at the PR description, found no tracking
link, concluded a governance rule had been violated, and created a fresh
`[Auto-Trace]` issue. Three separate defects compounded:

1. **No concurrency control.** Workflow runs for the same pull request executed in
   parallel and raced. Each run independently decided to create an issue, and none
   knew the others existed.
2. **No dedup search.** The creation path never asked whether a tracking issue for
   this pull request already existed. It simply created another one.
3. **A stale event payload.** The check that looked for a tracking link read the PR
   description from the webhook event that triggered the run. That payload is a
   snapshot taken when the event fired; it lags behind edits made through the
   GitHub UI or API. So even after an earlier run appended a tracking link, the
   next `synchronize` event still carried the old, link-less description and created
   yet another issue.

The result was issues #997 through #1054 — 52 identical auto-trace entries cluttering
the tracker and drowning the signal in noise.

## The fix

We fixed all three defects, because fixing only one would leave the flood possible.

### 1. Search before creating

The reviewer now searches open auto-trace issues for one matching the PR — by title,
or by a body that references this pull request number — and returns the canonical
issue if it finds one. The creation path reuses that canonical issue and links the PR
to it, and only creates a new issue when the search comes back empty. Creation is now
conditional on absence, which is what makes it idempotent.

### 2. Read the live PR body, not the event payload

Instead of trusting the webhook snapshot, the reviewer fetches the current PR
description from the GitHub API at check time. Now a link added by a previous run is
visible, so a later `synchronize` event short-circuits: it sees the link, decides
there is nothing to do, and exits. This closes the staleness window that produced
most of the duplicates.

### 3. Serialize runs

The review workflow gained a concurrency block keyed per pull request, with
cancel-in-progress enabled. A new push cancels the in-progress review, so only the
latest commit is evaluated and two runs cannot race to create the same issue.

## The rule

All three behaviors are codified as a mandatory governance rule so a future edit
cannot silently remove one. The rule states: never create a second auto-trace issue
for a PR that already has one — reuse the canonical; use the current PR body from
the API, not the stale event payload; and rely on workflow concurrency to serialize
parallel pushes.

## The cleanup

The 52 duplicates were closed as duplicates of the canonical issue, and the original
pull request was linked to it. Only four legitimate auto-trace trackers remained open,
each for a distinct pull request. We verified the fix by running the dedup search
against the offending PR: it returned the existing issue number, proving that the
flood would now be impossible.

Governance tooling that creates noise is worse than no tooling. This rule keeps the
reviewer honest, silent, and idempotent — the automation posture in
[docs/STANDARDS.md](../../STANDARDS.md).

## References / Dig deeper

- Spec: `openspec/changes/steering-rule-90-auto-trace-dedup`.
- The mandatory rule lives in `openspec/config.yaml`.
- Tracking issue: [#1057](https://github.com/alsotoes/momo/issues/1057).
- Fix pull request: [#1058](https://github.com/alsotoes/momo/pull/1058).
- Reviewer logic: `ai_reviewer.py` (`find_existing_auto_trace`,
  `get_current_pr_body`, `create_missing_issue`) and `.github/workflows/gemini_reviewer.yml`.
- Sibling posts: [015: The Sentinel Sweep](015-sentinel-security-audit.md),
  [024: Bolt Performance Engineering](024-bolt-performance-engineering.md).
