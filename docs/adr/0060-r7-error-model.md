# 0060-r7-error-model

## Status
Proposed

## Confidence
Low

## Context
Current Momo error handling has three production-readiness gaps:

1. **No disk-full signaling** — `ENOSPC` is never emitted; writes silently fail or panic instead of returning a clean `ENOSPC` that clients/S3 SDKs can act on
2. **Single exit code** — all fatal exits use `exit 1`; no distinction between config error, network failure, disk full, etc., making automated orchestration/alerting impossible
3. **No cluster health introspection** — operators have no `/health` sub-endpoint that reports: node state, replication status, disk usage, peer membership, open leases, scrub progress

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #935
- PR: 
- Spec: `openspec/changes/r7-error-model/`
- Blog: 

