# Change: Reviewer REST mutations + robust approval detection

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1140 (tracking)

## Why

The AI reviewer (`.github/scripts/ai_reviewer.py`) runs on every PR but two
defects make it partially ineffective (observed on PR #1132 and #1136):

1. **All `gh pr edit` mutations silently fail.** The PAT (`PERSONAL_ACCESS_TOKEN`)
   carries only the `repo` scope, but `gh pr edit` / `gh issue edit` run GraphQL
   queries that request org-scoped fields, so every label/assignee/body mutation
   fails:
   `GraphQL: The 'login' field requires ['read:org'], but your token has only been granted: ['repo']`.
   Result: PRs get no auto-labels, no assignee, and no `Resolves #NNN` body link.

2. **Approved PRs never auto-merge.** The gate is
   `is_approved = review.strip().startswith("✅")`. Jules reviews are prefixed
   with `@google-labs-jules` and/or structured headings, so the approval marker
   never sits at position 0 → `check_ci_and_merge` never runs for Jules PRs.

## What Changes

- Replace every `gh pr edit` / `gh issue edit` / `gh issue create` call with
  REST helpers built on `gh api` (labels, assignee, PR body, issue creation),
  which the `repo` scope covers.
- Add `review_is_approved()`: ignore any appended ECC advisory block, strip a
  leading `@google-labs-jules` mention, never treat an explicit blocker
  (`🚨`/`🛑`/`VIOLATION`) as approval, and accept either a leading `✅` or the
  canonical approval sentence.
- Label auto-trace issues by category: `bug` for Sentinel (security/bug) PRs,
  `enhancement` otherwise (always `automation`).
- Add unit tests for the approval detector and the REST helpers.

## Non-Goals

- No change to the review prompt or the model.
- No change to the merge command (`gh pr merge --auto` already works with the
  `repo` scope).
- Widening the PAT scopes is a separate operational action (the REST path makes
  the reviewer token-agnostic regardless).

## Impact

- **Affected Specs:** `specs/reviewer-rest-mutations/spec.md`.
- **Behavior:** labels/assignee/body edits succeed; approved PRs (including
  Jules PRs) auto-merge; blocker reviews still withhold merge.
- **Security:** no new token scope is required; the reviewer keeps using the
  least-privilege `repo` scope.
