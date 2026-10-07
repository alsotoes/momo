> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1129

# adaptive-systems-foundations Specification

## Purpose

Implement foundational adaptive biological behaviors for Momo:
1. Ant Colony Optimization (Principle 3 from `docs/momofs/ADAPTIVE_SYSTEMS.md`) for client read routing and automatic multi-replica fallback.
2. Epigenetics (Principle 6) and Homeostasis (Principle 10) for server-side concurrency sizing and dynamic connection admission backpressure.

## ADDED Requirements

### Requirement: Ant Colony Pheromone Tracking
The client layer SHALL provide a compile-time Go interface seam `PheromoneRouter` that tracks replica health using pheromones ($\tau$). Successful downloads SHALL strengthen a replica's pheromone trail, failures SHALL penalize the trail, and idle time SHALL apply exponential decay.

#### Scenario: Pheromone reinforcement on fast download
- **GIVEN** a node `N` with initial pheromone $\tau = 1.0$
- **WHEN** a download from `N` completes successfully in 20ms
- **THEN** `N`'s pheromone increases proportionally and is bounded by $\tau_{\max} = 10.0$

#### Scenario: Pheromone penalty on connection or download failure
- **GIVEN** a node `N` with current pheromone $\tau = 2.5$
- **WHEN** a download from `N` encounters a network dial error, timeout, or non-200 error status
- **THEN** `N`'s pheromone is reduced by a factor of 0.2 down to at least $\tau_{\min} = 0.05$

#### Scenario: Pheromone time-based evaporation
- **GIVEN** multiple nodes with varied pheromone levels
- **WHEN** time passes without traffic or an explicit evaporation interval triggers
- **THEN** all node pheromones decay exponentially ($\tau \leftarrow \tau \times 0.95$) toward base level

### Requirement: Weighted Replica Selection
`PheromoneRouter.SelectReplica` SHALL use weighted probabilistic selection (roulette-wheel) over the candidate replica nodes derived from CRUSH placement, ensuring fast nodes receive priority while newly joined or recovering nodes remain eligible for exploration.

#### Scenario: Fast node selected with high probability
- **GIVEN** node `A` with pheromone $5.0$ and node `B` with pheromone $0.5$
- **WHEN** `SelectReplica([]*common.Node{A, B})` is invoked over many iterations
- **THEN** node `A` is chosen approximately 10x more frequently than node `B` without completely starving node `B`

#### Scenario: Fallback when candidate list is empty
- **GIVEN** an empty replica slice or invalid parameters
- **WHEN** `SelectReplica` is called
- **THEN** `SelectReplica` returns `syscall.EINVAL` and does not panic

### Requirement: Resilient Multi-Replica Fallback
`client.DownloadWithFallback` SHALL calculate the placement node set for the object's content hash using Momo's CRUSH algorithm and attempt retrieval from the preferred replica. If the preferred replica fails, it SHALL record a pheromone penalty and transparently attempt sibling replicas until retrieval succeeds or all replicas are exhausted.

#### Scenario: Primary replica down with transparent secondary fallback
- **GIVEN** an object with CRUSH replicas `[Node 0, Node 1, Node 2]` where Node 0 is down and Node 1 is healthy
- **WHEN** `client.DownloadWithFallback` is called
- **THEN** it attempts Node 0, detects failure, penalizes Node 0's pheromone, successfully retrieves the object from Node 1, and returns nil error to the caller

#### Scenario: All replicas unavailable
- **GIVEN** an object where all CRUSH replicas are unreachable
- **WHEN** `client.DownloadWithFallback` is called
- **THEN** it returns an error wrapping `syscall.EHOSTUNREACH` after exhausting all candidate nodes

### Requirement: Epigenetic Hardware Probing on Boot
The server layer SHALL probe host resource constraints on startup (cgroups memory limit, OS file descriptor limits via `rlimit`, and CPU count) to calculate an initial concurrency capacity ceiling, replacing hardcoded constants.

#### Scenario: Container with constrained memory ceiling
- **GIVEN** a container environment with cgroups memory limit of 256MB
- **WHEN** `NewAdaptiveConcurrencyController` initializes
- **THEN** the initial connection capacity is capped to prevent OOM (e.g., $\le 64$ slots) rather than defaulting to 1000

#### Scenario: High-memory bare-metal server
- **GIVEN** a host with 64GB available RAM and 65,536 file descriptors
- **WHEN** `NewAdaptiveConcurrencyController` initializes
- **THEN** the initial capacity scales up to its maximum ceiling (e.g., 4096+ slots)

### Requirement: Homeostatic Connection Admission
`AdaptiveConcurrencyController.AcquireSlot` SHALL dispense connection handling tokens. Under high system load (CPU > 85% or low free memory), it SHALL apply backpressure by rejecting or queueing incoming connections before memory exhaustion occurs.

#### Scenario: Normal operating conditions
- **GIVEN** system CPU < 50% and ample memory headroom
- **WHEN** `AcquireSlot(ctx)` is called
- **THEN** it grants a slot immediately and returns a release function

#### Scenario: Severe resource saturation
- **GIVEN** host memory usage approaching container OOM limits
- **WHEN** `AcquireSlot(ctx)` is called
- **THEN** it denies the slot or blocks until deadline, returning `syscall.EBUSY` or `syscall.ETIMEDOUT` to protect daemon survival

### Requirement: Unified Panic Recovery & POSIX Compliance
All methods in `PheromoneRouter` and `AdaptiveConcurrencyController` SHALL implement defensive two-line panic recovery (Rule 4 / Rule 37) logging via `log.Printf` and formatting return errors with POSIX `syscall` constants.

#### Scenario: Panic in replica selection
- **GIVEN** unexpected nil pointer or corrupted node slice
- **WHEN** `SelectReplica` panics
- **THEN** the panic is safely caught, logged, and returned as `syscall.EIO`
