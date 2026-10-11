# 0084-post-merge-master-gate-verification

## Status
Proposed

## Confidence
Medium

## Context
Third recurrence of the same failure class (after #1156/#1159 and the #1164 follow-up chain): a PR merges with a green PR gate, then master's SonarCloud quality gate flips to ERROR after the post-merge analysis. Two compounding causes:

1. **Sonar leak-period asymmetry** — PR analysis diffs against master; master's gate evaluates a leak-period baseline. Moved or reformatted code is re-dated as "new," so issues invisible to the PR diff can fail master post-merge.
2. **Premature done-declaration** — the agent reports "master green, Sonar OK" from PR-level evidence and a stale master snapshot instead of waiting for the master gate on the merge SHA to actually settle.

Process rules today cover pre-merge discipline (Rule 71: never branch from red master) but say nothing about **post-merge** verification, leaving the exact moment the failure class occurs ungoverned.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1184
- PR: 
- Spec: `openspec/changes/post-merge-master-gate-verification/`
- Blog: 

