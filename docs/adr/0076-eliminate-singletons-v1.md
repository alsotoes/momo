# 0076-eliminate-singletons-v1

## Status
Proposed

## Confidence
Low

## Context
Current Momo has **singletons and package-level globals** that violate:
- **Diversity is Strength** (ADAPTIVE_SYSTEMS.md): Monoculture is fragile
- **Stigmergy**: Local rules need local state, not shared globals
- **Zero SPOF**: Global state = implicit coordination
- **Rule 74 (Seam-Over-Plugins)**: Compile-time seams, not runtime globals

| Location | Global | Problem |
|----------|--------|---------|
| `src/client/pheromone.go:70` | `DefaultRouter = NewPheromoneRouter()` | Package singleton; tests can't inject mock |
| `src/server/replication.go:32` | `var payloadPool = &sync.Pool{...}` | Global pool; all nodes share sizing |
| `src/server/replication.go:38-44` | `replicationStateMutex`, `currentReplicationMode`, `replicationState` | Global replication state |
| `src/metrics/metrics.go:138` | `serverId != 0` early return | Hardcoded coordinator role |
| Multiple files | Hardcoded constants (timeouts, capacities, thresholds) | Not adaptive, not testable |

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
- Issue: #1149
- PR: 
- Spec: `openspec/changes/eliminate-singletons-v1/`
- Blog: 

