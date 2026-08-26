> GitHub Issue URL: https://github.com/alsotoes/momo/issues/944

# steering-three-dot-diff-gate Specification

## Purpose
Make PR-scope determination merge-safe (three-dot diff), harden the merge gate
against rogue parallel-agent commits, document a CI-status freeze workaround,
and clarify the Rule 73 spec bar for performance changes.

## Steering Rules

### SR1: Rule 62 — stale review detection uses three-dot diff
The staleness check in Rule 62 MUST use `git diff master...HEAD --name-only`
(three-dot: the PR's own commits relative to the shared merge-base) instead of
two-dot `gh pr diff --name-only`. Rationale: after Rule 50 merges `master` into
a PR branch, two-dot diff inflates to include all merged master files and would
cause both false staleness verdicts and spurious Rule 47/73 findings.

### SR2: Rule 55 — pre-merge anti-rogue scope gate
Before merging, the agent MUST verify `git diff master...HEAD --name-only`
contains ONLY the PR's intended files. Any unexpected deletion or alteration of
a master file (e.g., a rogue parallel-agent commit, per Rule 66) MUST be
investigated and reverted before the merge proceeds.

### SR3: Rule 73 — perf-with-benchmark is an enhancement
Performance optimizations that add or change a benchmark surface do NOT qualify
for the trivial-refactor exemption in Rule 73. They are treated as feature
enhancements: full OpenSpec change set (`proposal.md`, `specs/<id>/spec.md`,
`tasks.md`) linked to a tracking issue via `Resolves #ISSUE_ID` is required.
This documents the existing reviewer behavior (enforced in
`.github/scripts/ai_reviewer.py`, which already uses three-dot diff at lines 13
and 161) without changing the spec gate.

## Documentation (docs/AI_FLYING_SOLO.md)

Add to "Common Pitfalls & Solutions":

### WP1: Merge-inflation (three-dot vs two-dot)
After Rule 50 merges `master` into a PR branch, `gh pr view --json files` and
`gh pr diff --name-only` (two-dot) list ALL of master's merged files as if
PR-authored. Always use three-dot `git diff master...HEAD --name-only` to
enumerate a PR's *own* changes before evaluating reviewer findings or merging.

### WP2: Frozen gh pr checks status
`gh pr checks` may show a check (e.g., `benchstat`) as `pending` for 15+ minutes
with a frozen `updatedAt` while the underlying job is actually
`completed/success`. Before extended waiting, cross-check with
`gh api repos/<owner>/<repo>/actions/runs/<run-id>/jobs` and read the job's
`steps[].conclusion`.

## Success Criteria

- `openspec/config.yaml`: Rules 62, 55, 73 text updated per SR1-SR3.
- `docs/AI_FLYING_SOLO.md`: WP1 and WP2 pitfalls present.
- Reviewer script re-audited: three-dot diff confirmed at lines 13 and 161.
- This change set ships with the PR and links issue #944.
