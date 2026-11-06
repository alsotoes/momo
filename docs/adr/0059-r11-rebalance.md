# 0059-r11-rebalance

## Status
Superseded

> Superseded by ADR 0072 (`docs/adr/0072-r11-stigmergic-rebalance.md`): a
> coordinator-driven rebalance reintroduces the single point of failure the
> metadata layer was designed to avoid, and contradicts the stigmergic
> local-rule model in `docs/CORE/ADAPTIVE_SYSTEMS.md`. The spec was archived at
> `openspec/changes/archive/2026-11-06-r11-rebalance/`.

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
- Spec: `openspec/changes/archive/2026-11-06-r11-rebalance/` (archived, superseded)
- Superseded by: docs/adr/0072-r11-stigmergic-rebalance.md
- Blog: 

