# 0059-r11-rebalance

## Status
Proposed

## Confidence
Low

## Context
When a node joins or leaves the cluster, the CRUSH placement changes — some objects now map to different replica sets. Currently Momo has no automatic rebalancing: the new topology is used only for new writes, while existing data stays on old replicas. This leads to:

1. **Unbalanced load** — new node receives only new writes; old nodes keep all old data
2. **Under-replication** — if a node leaves, its replicas are not rebuilt elsewhere
3. **Over-replication** — if a node joins, old data isn't moved off overloaded nodes

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
- Issue: #939
- PR: 
- Spec: `openspec/changes/r11-rebalance/`
- Blog: 

