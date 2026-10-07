# Proposal: Adaptive Systems Foundations (Ant-Colony Routing & Epigenetic Concurrency)

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1129

- **Champion:** alsotoes
- **Status:** `Proposed`

## 1. Problem

Momo's operational architecture contains rigid, static points that compromise resilience under dynamic real-world workloads and constrained hardware:

1. **Single-Node Client Failures:** `client.Download` (`src/client/client.go:486`) targets a single, hardcoded daemon ID. If that node is congested, slow, or restarting, the download fails immediately with an error, even though healthy replicas exist across the cluster according to CRUSH placement.
2. **Deterministic Hot-Spotting:** `PeerMap.AliveByQuality` (`src/p2p/peer_map.go:91`) sorts peers strictly ascending by EWMA RTT. It sends 100% of traffic to the single lowest-RTT peer, causing artificial hot-spots, and starves freshly joined peers (RTT 0) from exploration.
3. **Hardcoded Server Concurrency:** `const maxConcurrentConnections = 1000` (`src/server/server.go:219`) statically allocates a 1000-slot semaphore. On low-memory edge devices or constrained containers (e.g. 256MB RAM), 1000 concurrent streaming uploads exhaust host memory and cause OOM kills. On high-end 64-core bare-metal servers, 1000 connections serves as an artificial bottleneck.

These rigidities directly violate the biological design principles established in `docs/momofs/ADAPTIVE_SYSTEMS.md`:
- **Principle 3 (Ant Colony Optimization):** Replica selection should adapt based on pheromone strength learned over time, balancing exploration and exploitation.
- **Principle 6 (Epigenetics) & Principle 10 (Homeostasis):** The same binary should adapt its resource footprint and concurrency limits to its host environment, maintaining equilibrium through feedback loops.

## 2. Proposed Solution

Implement **Phase 1: Adaptive Systems Foundations** adhering strictly to **Rule 74 (Seam-over-plugins)**:

### 2.1 Ant Colony Pheromone Routing (`PheromoneRouter`)
Introduce a compile-time Go interface seam `client.PheromoneRouter` (`src/client/pheromone.go`):
- **Pheromone State ($\tau_i$):** Every replica starts with neutral pheromone $\tau_0 = 1.0$, clamped to $[\tau_{\min}, \tau_{\max}] = [0.05, 10.0]$.
- **Reinforcement on Success:** Fast completions strengthen the pheromone trail:
  $$\tau_i \leftarrow \min\left(10.0, \tau_i + \min\left(2.0, \frac{100\text{ms}}{\text{rtt}}\right)\right)$$
- **Penalty on Failure:** Connection refusals, timeouts, or non-200 responses penalize the trail:
  $$\tau_i \leftarrow \max(0.05, \tau_i \times 0.2)$$
- **Evaporation (Decay):** Periodic time-based evaporation decays old successes towards base:
  $$\tau_i \leftarrow \max(0.05, \tau_i \times 0.95)$$
- **Exploration vs. Exploitation:** Weighted roulette-wheel selection ensures high-reputation nodes receive the majority of traffic while newly joined or recovering nodes still receive exploratory traffic.
- **Resilient Fallback:** `client.DownloadWithFallback` evaluates the CRUSH replica set for the content hash, queries `PheromoneRouter`, and automatically falls back to sibling replicas if the selected node fails.

### 2.2 Epigenetic Server Concurrency (`AdaptiveConcurrencyController`)
Introduce a compile-time Go interface seam `server.AdaptiveConcurrencyController` (`src/server/concurrency.go`):
- **Epigenetic Hardware Probing on Boot:** Probes cgroups memory limit (`/sys/fs/cgroup/memory.max` or `/sys/fs/cgroup/memory/memory.limit_in_bytes`), file descriptor limits via `unix.Getrlimit(unix.RLIMIT_NOFILE)`, and CPU count via `runtime.NumCPU()`.
- **Calculated Capacity:** Initial concurrency ceiling calculated as:
  $$\text{Capacity} = \text{clamp}\left(\min\left(\frac{\text{FD Limit}}{4}, \frac{\text{Available RAM}}{4\text{MB}}\right), 32, 10000\right)$$
- **Homeostatic Backpressure:** Dynamically monitors memory allocation and CPU pressure. Under high pressure (>85% CPU or low free memory), acquisition throttles connections gracefully rather than crashing with OOM.

## 3. Architecture & Seam Design (Rule 74)

```
+-----------------------------------------------------------------------------------+
|                           Adaptive Systems Foundations                            |
+-----------------------------------------------------------------------------------+
|                                                                                   |
|  [Client Layer]                                                                   |
|    client.DownloadWithFallback                                                    |
|          │                                                                        |
|          ├──► CRUSH Placement (Retrieves Replica Node Set)                         |
|          ├──► PheromoneRouter.SelectReplica (Ant Colony Weighted Selection)       |
|          │        ▲                                                               |
|          │        └── RecordSuccess / RecordFailure (Pheromone Feedback)          |
|          └──► Sibling Fallback Loop (Tries Next Replica on Dial/Read Error)       |
|                                                                                   |
|  [Server Layer]                                                                   |
|    server.Accept Loop                                                             |
|          │                                                                        |
|          ▼                                                                        |
|    AdaptiveConcurrencyController.AcquireSlot                                      |
|          │                                                                        |
|          ├──► Epigenetic Probing (RAM / rlimit / cgroups on boot)                 |
|          └──► Homeostatic Feedback (Adjusts active slots under system load)       |
|                                                                                   |
+-----------------------------------------------------------------------------------+
```

## 4. Why This Approach

1. **Zero Wire Breaking Changes:** 100% wire-compatible with `momo-tcp`, `momo-quic`, `s3-tcp`, and `s3-quic`.
2. **Immediate Availability Gain:** Endpoints downloading data survive single-node crashes or maintenance without client errors.
3. **Hardware Elasticity:** Automatically scales down to small IoT/Edge nodes (avoiding OOM) and up to multi-core bare-metal servers.
4. **Compile-Time Safety (Rule 74):** Uses concrete Go interface seams, avoiding external plugins or runtime reflection overhead.

## 5. Alternatives Considered

1. **Static Retry Loop:** Simply retry sequentially in CRUSH order ($0 \to 1 \to 2$). Rejected because it constantly hits dead nodes first, incurring timeout latency on every read until the primary returns.
2. **Dynamic Plugin (.so) Loading:** Considered in `ADAPTIVE_SYSTEMS.md` §12. Rejected per Rule 74 (Seam-over-plugins) due to stability, compiler compatibility, and memory safety invariants.
3. **Centralized Coordinator for Concurrency:** Have Node 0 dictate concurrency. Rejected per Rule 2 (Decentralized Primary) and Stigmergy principles.
