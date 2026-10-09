# 0080-panic-recovery-normalization

## Status
Accepted

## Confidence
High

## Context
Panic recovery (Rules 37 and 43) was implemented as ~90 hand-rolled
`defer func(){ if recover() ... }()` closures spread across `src/`. They
duplicated the same two-line pattern, drifted in message format, were
individually untestable, and in a few goroutine cases could repanic on a nil
dereference while handling the original panic.

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
- Issue: #1160
- PR: 
- Spec: `openspec/changes/panic-recovery-normalization/`
- Blog: 

