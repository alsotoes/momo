# 0073-r6-metadata-ha-v2

## Status
Proposed

## Confidence
Medium

## Context
Current MomoFS stores metadata (namespace→hash, object metadata, S3 headers) in a **per-node bbolt database** with no cross-node visibility. This creates three production-readiness gaps:

1. **No HA for metadata writes** — if a node crashes, its metadata is unavailable until recovery
2. **No backup/recovery** — no `momo backup/restore`, no automated snapshots, no documented PITR
3. **ListObjects scalability** — S3 ListObjectsV2 scatters to all N nodes (O(N) RPCs); with 100+ nodes this is prohibitive

The `distributed-metadata-v1` spec (ratified) defines a shard-owner + replica model. **This v2 revision aligns with core principles**: "Read From Any Node" and "Zero SPOF" require **any-replica quorum writes** and **any-node reads**, not a designated shard owner.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Partial
- **Blog post**: 

## References
- Issue: #1142
- PR: 
- Spec: `openspec/changes/r6-metadata-ha-v2/`
- Blog: 

