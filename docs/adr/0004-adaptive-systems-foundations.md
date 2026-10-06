# 0004-adaptive-systems-foundations

## Status
Accepted

## Confidence
High

## Context
Momo's operational architecture contains rigid, static points that compromise resilience under dynamic real-world workloads and constrained hardware:

1. **Single-Node Client Failures:** `client.Download` (`src/client/client.go:486`) targets a single, hardcoded daemon ID. If that node is congested, slow, or restarting, the download fails immediately with an error, even though healthy replicas exist across the cluster according to CRUSH placement.
2. **Deterministic Hot-Spotting:** `PeerMap.AliveByQuality` (`src/p2p/peer_map.go:91`) sorts peers strictly ascending by EWMA RTT. It sends 100% of traffic to the single lowest-RTT peer, causing artificial hot-spots, and starves freshly joined peers (RTT 0) from exploration.
3. **Hardcoded Server Concurrency:** `const maxConcurrentConnections = 1000` (`src/server/server.go:219`) statically allocates a 1000-slot semaphore. On low-memory edge devices or constrained containers (e.g. 256MB RAM), 1000 concurrent streaming uploads exhaust host memory and cause OOM kills. On high-end 64-core bare-metal servers, 1000 connections serves as an artificial bottleneck.

These rigidities directly violate the biological design principles established in `docs/momofs/ADAPTIVE_SYSTEMS.md`:
- **Principle 3 (Ant Colony Optimization):** Replica selection should adapt based on pheromone strength learned over time, balancing exploration and exploitation.
- **Principle 6 (Epigenetics) & Principle 10 (Homeostasis):** The same binary should adapt its resource footprint and concurrency limits to its host environment, maintaining equilibrium through feedback loops.

## Decision
- Ant Colony Pheromone Tracking: The client layer SHALL provide a compile-time Go interface seam `PheromoneRouter` that tracks replica health using pheromones ($\tau$). Successful downloads SHALL strengthen a replica's pheromone trail, failures SHALL penalize the trail, and idle time SHALL apply exponential decay.
- Weighted Replica Selection: `PheromoneRouter.SelectReplica` SHALL use weighted probabilistic selection (roulette-wheel) over the candidate replica nodes derived from CRUSH placement, ensuring fast nodes receive priority while newly joined or recovering nodes remain eligible for exploration.
- Resilient Multi-Replica Fallback: `client.DownloadWithFallback` SHALL calculate the placement node set for the object's content hash using Momo's CRUSH algorithm and attempt retrieval from the preferred replica. If the preferred replica fails, it SHALL record a pheromone penalty and transparently attempt sibling replicas until retrieval succeeds or all replicas are exhausted.
- Epigenetic Hardware Probing on Boot: The server layer SHALL probe host resource constraints on startup (cgroups memory limit, OS file descriptor limits via `rlimit`, and CPU count) to calculate an initial concurrency capacity ceiling, replacing hardcoded constants.
- Homeostatic Connection Admission: `AdaptiveConcurrencyController.AcquireSlot` SHALL dispense connection handling tokens. Under high system load (CPU > 85% or low free memory), it SHALL apply backpressure by rejecting or queueing incoming connections before memory exhaustion occurs.
- Unified Panic Recovery & POSIX Compliance: All methods in `PheromoneRouter` and `AdaptiveConcurrencyController` SHALL implement defensive two-line panic recovery (Rule 4 / Rule 37) logging via `log.Printf` and formatting return errors with POSIX `syscall` constants.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1129
- PR: 
- Spec: `openspec/changes/adaptive-systems-foundations/`
- Blog: 

