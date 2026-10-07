# 0065-distributed-coordination-v1

## Status
Proposed

## Confidence
Low

## Context
Current Momo has a **single point of failure for coordination**:

1. **Metrics Controller** (`src/metrics/metrics.go:138`): `if serverId != 0 { return }` — only node 0 runs the polymorphic replication mode switching logic
2. **Replication Propagation** (`src/server/replication.go:243`): `if 0 == serverId` — only node 0 propagates replication mode changes to peers

If node 0 dies:
- No replication mode switching under load (stuck in current mode)
- No replication mode change propagation to cluster
- No durability floor enforcement

This violates core principles:
- **Zero SPOF** (Principle 2): No coordinator, no master
- **Read From Any Node** (Principle 1): Any node should serve any coordination role
- **Stigmergy** (ADAPTIVE_SYSTEMS.md): Complex behavior from local rules, not central coordinator

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
- Issue: #1144
- PR: 
- Spec: `openspec/changes/distributed-coordination-v1/`
- Blog: 

