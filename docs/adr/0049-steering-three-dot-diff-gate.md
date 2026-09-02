# 0049-steering-three-dot-diff-gate

## Status
Accepted

## Confidence
High

## Context
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

## Decision


## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/steering-three-dot-diff-gate/
- Blog: docs/blog/posts/...md
