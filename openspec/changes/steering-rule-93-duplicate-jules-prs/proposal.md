# Change: Steering Rule 93 — Duplicate Jules PR/Branch Consolidation

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1121 (tracking)

## Why

Jules — the automated Bolt/Sentinel agent — opens **more than one PR and branch
for the same task** across parallel runs. During the aws-chunked zero-allocation
work, two PRs shipped the identical optimization:

- **#1112** "⚡ Bolt: Zero-allocation string parsing for AWS chunked headers"
  (merged as `60933b80`).
- **#1114** "⚡ Bolt: Eliminate heap allocations in parseAWSChunkHeader"
  (duplicate; same `parseAWSChunkHeader` change).

Each carried its own auto-trace issue (#1113, #1115) and its own stale branch.
After #1112 merged, #1114 sat open with a redundant tracker until manually
closed. The working tree also kept a stale remote branch
(`bolt/aws-chunked-zero-allocation-…`) that had to be pruned.

The existing rules do not cover this case:

- **Rule 80** (Jules PR takeover/rebuild) handles **one** PR that needs a clean
  rebuild — not two PRs doing the same thing.
- **Rule 90** (auto-trace dedup) makes the **reviewer** stop creating duplicate
  *issues* for one PR — it says nothing about duplicate *PRs*.

Without a rule, duplicates linger: the tracker shows two "open" efforts for one
task, CI runs twice, and a reader cannot tell which is canonical.

## What Changes

- Add **Rule 93 — Duplicate Jules PR/Branch Consolidation** to
  `openspec/config.yaml` (now rules 1–93).
- Add the Rule 93 reference and a pitfall entry to `docs/AI_FLYING_SOLO.md`.
- The rule: when Jules opens multiple PRs/branches for the same task —
  1. keep the **canonical** PR (the one that reaches merge / carries the
     tracking issue);
  2. close the duplicate with an explanatory comment
     (`gh pr close <dup> --delete-branch`) referencing the canonical PR/commit;
  3. close the duplicate's auto-trace issue as **superseded**, pointing at the
     canonical tracking issue (one canonical issue only, per Rules 81/90);
  4. prune the deleted remote branch (`git fetch --prune`), then confirm
     `gh pr list --state open` shows no related PRs.

## Non-Goals

- No change to the reviewer script (`ai_reviewer.py`) — Rule 90 already covers
  the reviewer's own duplication.
- No change to the Rule 80 rebuild workflow for a single PR.

## Impact

- **Governance only** — no application code, no behavioral change.
- **Relationship:** Rule 80 covers *one* PR rebuild; Rule 90 covers reviewer
  duplicate *issues*; Rule 93 covers *multiple PRs* for the *same task*.
