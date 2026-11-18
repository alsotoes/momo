# 0113-s3-multipart-restart-recovery

## Status
Proposed

## Confidence
Low

## Context
Multipart sessions lived only in the process map. The parts were on disk
(#1358), but after a restart nothing referenced them:

- the client's `CompleteMultipartUpload` answered `404 NoSuchUpload`, so a
  multi-GiB upload had to start over;
- the spill directory looked like an orphan, so the reaper (#1361) eventually
  deleted data the client still believed it owned.

A restart during a large upload — deploy, OOM kill, node reboot — is routine, and
silently discarding the work is expensive and surprising.

## Decision
- Sessions Are Durable: An open upload SHALL record its identity and parts in its spill directory.
- Sessions Are Recovered: The gateway SHALL rebuild its session map from manifests at startup, keeping upload ids stable.
- Recovery Is Conservative: 

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1371
- PR: 
- Spec: `openspec/changes/s3-multipart-restart-recovery/`
- Blog: 

