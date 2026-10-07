# 0017-adaptive-thresholds-v1

## Status
Proposed

## Confidence
Low

## Context
Current Momo has **15+ hardcoded constants** that violate "Adapt, don't configure" (ADAPTIVE_SYSTEMS.md: Epigenetics) and Stigmergy:

| Location | Constant | Current Value | Should Adapt To |
|----------|----------|---------------|-----------------|
| `src/common/struct.go:177-178` | `MinThreshold`/`MaxThreshold` | Static config (e.g., 0.2/0.8) | Cluster CPU/mem history (EWMA) |
| `src/client/pheromone.go:15-28` | All pheromone constants | Fixed (1.0, 0.95, 0.2, 10.0, 1s) | Observed RTT variance, cluster size |
| `src/server/replication.go:21` | `payloadPoolCapacity` | 1024 | Workload (blob size distribution) |
| `src/server/replication.go:143` | `maxConcurrentConnections` | 1000 | Available file descriptors, RAM |
| `src/client/pheromone.go:27` | `EvaporationInterval` | 1s | Cluster dynamics, RTT distribution |
| `src/server/server.go:335` | Handshake timeout | 10s | Network RTT percentile |
| `src/server/server.go:431` | Metadata deadline | 60s | Object size + network |
| `src/server/replication.go:271` | Propagation timeout | 11s | Cluster size + latency |

These are **epigenetic markers** — the same binary should behave differently based on environment and runtime conditions, not static config.

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
- Issue: #1145
- PR: 
- Spec: `openspec/changes/adaptive-thresholds-v1/`
- Blog: 

