# Change: Post-Merge Master Gate Verification (Rule 94)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1184

## Why

Third recurrence of the same failure class (after #1156/#1159 and the #1164 follow-up chain): a PR merges with a green PR gate, then master's SonarCloud quality gate flips to ERROR after the post-merge analysis. Two compounding causes:

1. **Sonar leak-period asymmetry** — PR analysis diffs against master; master's gate evaluates a leak-period baseline. Moved or reformatted code is re-dated as "new," so issues invisible to the PR diff can fail master post-merge.
2. **Premature done-declaration** — the agent reports "master green, Sonar OK" from PR-level evidence and a stale master snapshot instead of waiting for the master gate on the merge SHA to actually settle.

Process rules today cover pre-merge discipline (Rule 71: never branch from red master) but say nothing about **post-merge** verification, leaving the exact moment the failure class occurs ungoverned.

## What Changes

- New **Rule 94: Post-Merge Master Gate Verification** in `openspec/config.yaml`
  - Merging is not done. After every merge: wait for the full master run set on the merge commit AND SonarCloud master gate = OK before reporting completion.
  - PR checks are never acceptable evidence for "master is green."
  - A red post-merge gate is a same-session hotfix duty (fix → PR → re-verify → confirm OK), never left for the user to discover.
  - Local `golangci-lint` new-mode is necessary but not sufficient — the authoritative check is the master gate itself.
- `docs/AI_FLYING_SOLO.md`: final workflow step (post-merge gate watch) + pitfall entry.

## Impact

- **Affected specs:** new `post-merge-master-gate-verification` spec
- **Affected code:** `openspec/config.yaml` (Rule 94), `docs/AI_FLYING_SOLO.md` (step + pitfall). No Go code changes in this change.
- **ADR:** generated via `make adr-sync` (Rules 77/78). Rule 76: process/governance with no engineering decision → `no-blog` justification per the 2026-10-02 journal policy.
