# 0081-linting-complexity-gates

## Status
Accepted

## Confidence
High

## Context
Master's SonarCloud gate went red three times in a row after merges
(#1164 → #1167 → #1169), and every failure was the same rule: `go:S3776` —
cognitive complexity > 15 on new code. Each round surfaced only *after* the
merge, when the master pipeline turned red and required a follow-up PR.

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
- Issue: #1172
- PR: 
- Spec: `openspec/changes/linting-complexity-gates/`
- Blog: 

