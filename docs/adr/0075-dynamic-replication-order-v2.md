# 0075-dynamic-replication-order-v2

## Status
Proposed

## Confidence
Medium

## Context
Current Momo uses a **static `ReplicationOrder`** parsed from config (`src/common/config.go:358-380`). This violates:

- **Multi-Scheme Replication** (Principle 4): Configurable per-tenant/bucket, but order is global and static
- **Stigmergy** (ADAPTIVE_SYSTEMS.md): Complex behavior from local rules, not central static config
- **Adapt, don't configure** (Epigenetics): Order should optimize based on observed mode performance

The replication order determines the polymorphic controller's degradation/promotion path. A static order means:
- Poorly performing modes stay in the sequence
- Well-adapted modes can't be promoted
- No per-workload optimization

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Done
- **Blog post**: docs/blog/posts/075-dynamic-replication-order.md

## References
- Issue: #1146
- PR: 
- Spec: `openspec/changes/dynamic-replication-order-v2/`
- Blog: docs/blog/posts/075-dynamic-replication-order.md

