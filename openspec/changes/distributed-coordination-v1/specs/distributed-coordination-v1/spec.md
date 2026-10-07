# Distributed Coordination — Metrics Controller + Replication Propagation

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1144

## Purpose

Eliminate the single point of failure for cluster coordination by distributing the **Metrics Controller** and **Replication Propagator** roles across all nodes via lease-based leader election. Any node can serve as coordinator; the cluster self-heals if the current coordinator fails.

## Requirements

### Requirement 1: Coordination Role Leases

#### Scenario: Node campaigns for coordination role
**Given** a cluster with P2P gossip + lease manager enabled
**When** a node starts and `coordination.enabled=true`
**Then** the node attempts to acquire the `MetricsController` lease
**And** attempts to acquire the `ReplicationPropagator` lease
**And** lease TTL is configurable (`coordination.metrics_controller_lease_ttl`, `coordination.propagator_lease_ttl`)

#### Scenario: Lease acquisition
**Given** multiple nodes campaign for the same role
**When** a node's `AcquireLease` succeeds
**Then** that node becomes the lease holder for that role
**And** other nodes observe the lease holder via gossip
**And** only the lease holder performs the role's duties

#### Scenario: Lease renewal
**Given** a node holds a lease
**When** the lease TTL approaches expiry
**Then** the node renews the lease before expiry
**And** if renewal fails, the node releases the role

### Requirement 2: Distributed Metrics Controller

#### Scenario: Metrics controller runs on lease holder only
**Given** a node holds the `MetricsController` lease
**When** the metrics ticker fires
**Then** the node executes `checkMetricsAndSwap` and may switch replication mode
**And** non-lease-holders skip `checkMetricsAndSwap` (return early)

#### Scenario: Metrics controller failover
**Given** the current metrics controller lease holder fails (SWIM marks SUSPECT/OFFLINE)
**When** the lease expires or is revoked
**Then** a new election occurs among alive nodes
**And** the new lease holder resumes metrics controller duties
**And** `currentIndex` and `start` time are reconstructed from persisted state

#### Scenario: State persistence
**Given** a node holds the `MetricsController` lease
**When** `currentIndex` or `start` time changes
**Then** the new state is persisted to BoltDB (`replication_state` bucket)
**And** persisted at interval `coordination.state_persistence_interval`

#### Scenario: State reconstruction
**Given** a new node acquires the `MetricsController` lease
**When** the node starts its metrics loop
**Then** it reads `currentIndex` and `start` from BoltDB
**And** if no persisted state, initializes to defaults (index 0, now)

### Requirement 3: Distributed Replication Propagation

#### Scenario: Any node propagates replication mode change
**Given** any node receives a `ChangeReplicationMode` RPC (via `ChangeReplicationModeServer`)
**When** the RPC updates the local replication state
**Then** the node broadcasts the new mode to all peers via gossip
**And** no designated "primary" — all nodes participate

#### Scenario: Propagation deduplication
**Given** multiple nodes may receive the same mode change
**When** a node receives a propagation message
**Then** it checks if it already processed this mode+timestamp (via request ID)
**And** if duplicate, ignores; if new, processes and rebroadcasts
**And** request IDs are `mode:timestamp:originNodeID`

#### Scenario: Propagation acknowledgment
**Given** a node broadcasts a mode change
**When** peers receive and apply the mode change
**Then** peers send acknowledgment back to originator
**And** originator tracks acks with timeout (`coordination.propagation_ack_timeout`)

### Requirement 4: Lease Holder Health Monitoring

#### Scenario: Non-lease-holders monitor lease holder
**Given** a node does not hold a lease
**When** the lease holder is marked SUSPECT/OFFLINE by SWIM
**Then** the node prepares to campaign for the lease
**And** if lease expires, new election starts automatically

#### Scenario: Graceful lease release
**Given** a lease holder is shutting down or losing the lease
**When** the node releases the lease (explicit or TTL expiry)
**Then** it transfers any in-memory state to BoltDB
**And** logs the handoff for audit

### Requirement 5: Configuration

#### Scenario: Coordination configuration
**Given** `momo.conf` with `[coordination]` section
**When** config is loaded
**Then** the following keys are parsed with safe defaults:

```toml
[coordination]
enabled = true
metrics_controller_lease_ttl = "30s"
propagator_lease_ttl = "30s"
election_retry_interval = "5s"
state_persistence_interval = "10s"
propagation_ack_timeout = "10s"
```

### Requirement 6: Backward Compatibility

#### Scenario: Legacy mode
**Given** `coordination.enabled=false` (default for backward compat)
**When** the daemon starts
**Then** legacy behavior: node 0 runs metrics controller, node 0 propagates
**And** no lease election, no gossip-based coordination
**And** `serverId != 0` early returns preserved

## Configuration Schema

```go
type ConfigurationCoordination struct {
	Enabled                       bool          `ini:"enabled"`
	MetricsControllerLeaseTTL     time.Duration `ini:"metrics_controller_lease_ttl"`
	PropagatorLeaseTTL            time.Duration `ini:"propagator_lease_ttl"`
	ElectionRetryInterval         time.Duration `ini:"election_retry_interval"`
	StatePersistenceInterval      time.Duration `ini:"state_persistence_interval"`
	PropagationAckTimeout         time.Duration `ini:"propagation_ack_timeout"`
}
```

## BoltDB Schema Changes

New bucket: `replication_state` (per-node, not replicated)

```
Key: "metrics_controller" → Value: {CurrentIndex, StartTime, LastUpdated}
Key: "propagator"         → Value: {LastMode, LastTimestamp, LastUpdated}
```

## Integration Points

1. **LeaseManager** (`src/p2p/lease.go`): Used for role election
2. **Gossiper** (`src/p2p/gossip.go`): Advertises lease holder identity
3. **SWIM** (`src/p2p/peer_map.go`): Detects lease holder failure
4. **BoltDB** (`src/storage/storage.go`): Persists coordination state
5. **Metrics Loop** (`src/metrics/metrics.go`): Checks lease before acting
6. **Replication Propagation** (`src/server/replication.go`): Broadcasts via gossip

## Acceptance Criteria

- [ ] Node 0 dies → cluster continues switching replication modes under load
- [ ] Node 0 dies → replication mode changes propagate to all nodes
- [ ] Any node can become metrics controller via lease election
- [ ] Any node can propagate replication mode changes
- [ ] Lease holder failure → new election within 2x lease TTL
- [ ] State persisted → survives lease holder restart
- [ ] `coordination.enabled=false` → legacy behavior unchanged
- [ ] Chaos test: kill node 0 repeatedly → cluster self-heals
- [ ] Benchstat: no regression on replication mode switching latency

## Failure Scenarios

### Metrics Controller Lease Holder Dies
```
1. SWIM marks node 0 as SUSPECT (timeout)
2. Lease TTL expires (30s)
3. Other nodes campaign for MetricsController lease
4. Node 1 acquires lease, reads state from BoltDB
5. Node 1 resumes checkMetricsAndSwap loop
6. Cluster continues adapting replication mode
```

### Network Partition
```
1. Partition splits cluster into two groups
2. Each group elects its own lease holders (if quorum)
3. Lease TTL prevents split-brain (no quorum = no lease)
4. Partition heals → leases reconciled, single holder per role
```

### State Loss on Lease Holder
```
1. Node 1 holds lease, BoltDB corrupted
2. Node 1 loses lease, Node 2 acquires
3. Node 2 reads state → not found → initializes defaults
4. Safe: currentIndex=0 (highest durability), start=now
```

## Out of Scope

- R6 distributed metadata (separate spec)
- R11 stigmergic rebalance (separate spec)
- Full consensus (Raft/Paxos) — lease-based sufficient for these roles
- Cross-region coordination (Phase 7)