# 0034-r3-durability-consistency

## Status
Proposed

## Confidence
Medium

## Context
Momo's write path has no defined durability contract. A replica set acknowledges a write
without a stated fsync-before-ack guarantee, and a write that partially reaches some
replicas (mid-write failure) leaves inconsistent replica states. There is no documented
consistency model (reads may observe stale or partial data). The existing
`minimum_durability_factor` (#822) is a durability *floor* but does not define fsync-ack
semantics, survivor-set quorum, or linearizable/read-your-writes behavior.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/r3-durability-consistency/
- Blog: docs/blog/posts/...md
