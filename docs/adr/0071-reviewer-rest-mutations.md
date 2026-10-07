# 0071-reviewer-rest-mutations

## Status
Accepted

## Confidence
High

## Context
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

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1140
- PR: 
- Spec: `openspec/changes/reviewer-rest-mutations/`
- Blog: 

