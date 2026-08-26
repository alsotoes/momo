# Change: steering-three-dot-diff-gate — three-dot diff for PR scope, anti-rogue merge gate, CI-freeze notes

**Related Issues:**
- https://github.com/alsotoes/momo/issues/944

## Why

Manual takeover of PR #899 exposed three workflow gaps rooted in the difference
between **two-dot** and **three-dot** git diffs, plus a fragile merge gate:

1. After Rule 50 merges `master` into an external PR branch, two-dot diff
   (`gh pr diff --name-only`, `gh pr view --json files`) inflates to include
   EVERY merged master file as if PR-authored. The reviewer then evaluated
   merged-in master files (checksum.go, exporter, `openspec/*`) as Rule 47
   deletions and blocked the PR on non-existent violations.
2. A STOP comment (Rule 66) posted after the circuit breaker tripped did NOT
   actually stop a rogue parallel Jules commit (`e166a44e`) that deleted master
   files. The only effective safety net is verifying, before merge, that the
   PR's three-dot diff contains only intended files.
3. `gh pr checks` showed `benchstat` as `pending` for 15+ minutes with a frozen
   `updatedAt`, while the job was actually `completed/success` (confirmed via
   `gh api .../jobs`). Rule 55's "wait until none pending" can deadlock on a
   stale check status.

## What

- **Rule 55 (Merge Gate):** add an explicit pre-merge gate to verify the
  three-dot diff (`git diff master...HEAD --name-only`) contains ONLY the PR's
  intended files; unexpected master-file deletion/alteration MUST be
  investigated and reverted before merge.
- **Rule 62 (Staleness):** use three-dot `git diff master...HEAD --name-only`
  instead of two-dot `gh pr diff --name-only` when checking whether a review
  references stale files.
- **Rule 73 (Spec-First):** document that performance optimizations *with a
  benchmark surface* do NOT qualify for the trivial-refactor exemption — they
  are treated as enhancements (full OpenSpec + linked issue required). No
  behavioral change to the spec gate itself.
- **docs/AI_FLYING_SOLO.md:** add two Common Pitfalls — (a) merge-inflation
  (use three-dot to enumerate a PR's own changes), (b) frozen `gh pr checks`
  (cross-check `gh api .../jobs` before extended waiting).

## Goals / Non-Goals

- **Goals:** make PR-scope determination merge-safe and unambiguous; harden the
  merge gate against rogue parallel-agent commits; document the CI-status
  freeze workaround; clarify the Rule 73 bar for perf changes.
- **Non-Goals:** no change to the reviewer script (`.github/scripts/ai_reviewer.py`
  already uses three-dot at lines 13 and 161 — Rule 70 satisfied, documented in
  the PR comment). No change to Rule 66 (STOP) behavior.

## Success Criteria

- Rule 62/55/73 text in `openspec/config.yaml` updated; `docs/AI_FLYING_SOLO.md`
  pitfalls added.
- Reviewer script re-audited to confirm three-dot usage (no diff-scope bug).
- PR includes this OpenSpec change + `Resolves #944`.
