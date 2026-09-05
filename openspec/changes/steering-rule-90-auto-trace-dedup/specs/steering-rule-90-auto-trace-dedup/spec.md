> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1057

# steering-rule-90-auto-trace-dedup Specification

## Purpose

Codify as a mandatory steering rule that the AI reviewer MUST never create duplicate
auto-trace tracking issues for the same PR, and MUST use the current PR body plus workflow
concurrency control so the #1057 incident (52 duplicates for one PR) cannot recur.

## ADDED Requirements

### Requirement: no duplicate auto-trace issues per PR

The AI reviewer SHALL NOT create a second auto-trace tracking issue for a PR that already
has one.

#### Scenario: PR already tracked by an auto-trace issue
- **GIVEN** an OPEN auto-trace issue titled `[Auto-Trace] <PR title>` (or a body
  referencing `for PR #<n>`) exists for the PR
- **WHEN** the reviewer detects a Rule 11 traceability gap
- **THEN** the reviewer reuses that canonical issue
- **AND** links the PR body with `Resolves #<canonical>`
- **AND** does NOT create a new issue

#### Scenario: no existing auto-trace issue
- **GIVEN** no OPEN auto-trace issue exists for the PR
- **WHEN** the reviewer detects a Rule 11 traceability gap
- **THEN** the reviewer creates exactly one auto-trace issue
- **AND** links it with `Resolves <issue-url>`

### Requirement: current PR body for issue-link detection

The reviewer SHALL determine whether the PR body carries an issue link from the CURRENT
PR body fetched via the GitHub API, not from the (possibly stale) webhook event payload.

#### Scenario: body edited by a previous reviewer run
- **GIVEN** a previous reviewer run appended `Resolves #<canonical>` via `gh pr edit`
- **WHEN** a later `synchronize` event triggers a new review
- **THEN** the reviewer reads the current PR body (API)
- **AND** recognizes the existing `Resolves` link
- **AND** does not create a duplicate auto-trace issue

### Requirement: workflow concurrency control

The reviewer workflow SHALL serialize runs per PR so parallel pushes cannot race.

#### Scenario: two pushes arrive close together
- **GIVEN** a PR receives a second push while a review is in progress
- **WHEN** the workflow schedules the new run
- **THEN** the in-progress run is cancelled (`concurrency.cancel-in-progress: true`)
- **AND** only the latest commit is reviewed

## REMOVED Requirements

None.

## Acceptance Criteria

1. `ai_reviewer.py` exposes `find_existing_auto_trace` and `get_current_pr_body`.
2. `create_missing_issue` reuses an existing canonical auto-trace issue (verified by the
   #1057 incident reproduction: search for PR #996 returns an existing issue number).
3. `gemini_reviewer.yml` declares `concurrency` with `cancel-in-progress: true` keyed by PR.
4. Rule 90 present in `openspec/config.yaml` after Rule 89.
5. `docs/AI_FLYING_SOLO.md` references Rule 90 (reference list, Step 1c, Rule 80 protocol,
   pitfall entry).