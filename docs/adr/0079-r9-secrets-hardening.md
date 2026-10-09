# 0079-r9-secrets-hardening

## Status
Accepted

## Confidence
High

## Context
The initial R9 secrets-management delivery (`46b53132`, #937) failed the
SonarQube quality gate (new-code coverage 38.9%, Minor vulnerabilities,
Critical code smells). Investigation surfaced real defects beyond the missing
tests: `RotationManager.Rotate` always failed because it stored empty key
material, `loadSecretsConfig` never read per-source child sections, and the
`/reload-secrets` endpoint was wired before its rotation manager existed, so it
was a permanent no-op.

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
- Issue: #1153
- PR: 
- Spec: `openspec/changes/r9-secrets-hardening/`
- Blog: 

