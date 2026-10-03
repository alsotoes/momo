# Spec: Steering Rule 93 — Duplicate Jules PR/Branch Consolidation

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1121

## Requirements

### SR93-A1: Rule 93 documented in the steering-rules source of truth

**Requirement:** `openspec/config.yaml` MUST contain a Rule 93 describing
duplicate Jules PR/branch consolidation, as the single source of truth
(Rule 39).

**Scenario: The rule list is complete and parseable**

Given the steering rules in `openspec/config.yaml`,
When the configuration is parsed,
Then a rule numbered 93 exists and describes how to consolidate duplicate
Jules PRs/branches,
And the YAML still parses (no syntax error).

### SR93-A2: One canonical PR per task

**Requirement:** When multiple PRs implement the same task, exactly one MUST be
kept as canonical; the others MUST be closed as duplicates.

**Scenario: A duplicate Jules PR is detected**

Given PR A and PR B implement the same change (same diff/title),
And PR A is merged (or chosen as canonical),
When the duplicate is handled,
Then PR B is closed with an explanatory comment referencing PR A / its merge
commit,
And PR B's branch is deleted (`gh pr close <dup> --delete-branch`).

### SR93-A3: One canonical tracking issue per task

**Requirement:** The duplicate's auto-trace issue MUST be closed as superseded,
leaving a single canonical tracking issue.

**Scenario: Duplicate auto-trace issues exist**

Given auto-trace issue X for the canonical PR and issue Y for the duplicate,
When the duplicate is consolidated,
Then issue Y is closed with a comment pointing at X (the canonical tracker),
And issue X remains open/closed per the canonical PR's lifecycle.

### SR93-A4: No stale branches remain

**Requirement:** After consolidation, no branch for a closed duplicate PR MUST
remain on the remote.

**Scenario: Remote branch is pruned**

Given the duplicate PR's branch was deleted on GitHub,
When the consolidation finishes,
Then a `git fetch --prune` removes the stale remote ref,
And `gh pr list --state open` shows no duplicate for the task.

## Non-Goals

- No change to `ai_reviewer.py` (Rule 90 territory).
- No change to the single-PR rebuild flow (Rule 80).
