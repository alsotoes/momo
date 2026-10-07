# Coordinator Failover — SWIM-Based Leadership Election for Coordination Roles

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1147

## Purpose

Complete the distributed coordination failover story by implementing **SWIM-integrated lease failure detection**, **proactive leadership handoff**, **split-brain prevention**, and **state synchronization** for coordination roles (`MetricsController`, `ReplicationPropagator`).

## Requirements

### Requirement 1: SWIM-Integrated Lease Failure Detection

#### Scenario: Lease holder fails → immediate detection
**Given** a node holds a coordination lease
**When** SWIM marks that node SUSPECT or OFFLINE
**Then** the `LeaseManager` immediately checks if the failed node holds any leases
**And** if so, triggers lease revocation (bypassing normal TTL expiry)
**And** failover time reduced from `lease TTL` (30s) to `SWIM suspicion timeout` (~5s)

#### Scenario: Lease revocation on peer failure
**Given** `LeaseManager` detects peer SUSPECT/OFFLINE
**When** the peer holds a lease
**Then** `LeaseManager.RevokeLease(peerID, role)` is called
**And** lease is marked revoked in local state
**And** gossip broadcasts revocation to cluster

### Requirement 2: Proactive Leadership Handoff

#### Scenario: Lease holder health degrades → proactive resignation
**Given** a node holds a coordination lease
**When** local health metrics exceed thresholds:
- CPU > `proactive_resign_threshold_cpu` (default 90%)
- Memory > `proactive_resign_threshold_mem` (default 90%)
- Goroutines > `proactive_resign_threshold_goroutines` (default 100000)
**Then** the node initiates graceful lease resignation:
1. Persists all role state to BoltDB
2. Broadcasts "resigning" intent via gossip
3. Releases lease via `LeaseManager`
4. Logs handoff for audit

#### Scenario: Health monitoring
**Given** a node holds a lease
**When** `health_check_interval` elapses (default 10s)
**Then** node evaluates local health metrics
**And** if any threshold exceeded, initiates resignation
**And** logs health status for observability

### Requirement 3: Split-Brain Prevention

#### Scenario: Quorum required for lease acquisition
**Given** a node campaigns for a coordination role
**When** `split_brain_prevention=true` (default)
**Then** `LeaseManager.AcquireLease` requires quorum:
- Quorum = `(alive_peer_count / 2) + 1`
- Lease only granted if quorum of peers acknowledge
- If quorum not met: lease acquisition fails, node waits and retries

#### Scenario: Quorum lost → coordination suspended
**Given** cluster loses quorum (network partition, multiple failures)
**When** alive peer count < `(total_peers / 2) + 1`
**Then** all coordination leases are released
**And** cluster enters "coordination suspended" state:
- No metrics controller active (replication mode frozen)
- No replication propagator active (mode changes not propagated)
- Existing replication modes continue operating
- Logs warning: "Coordination suspended: insufficient quorum"

#### Scenario: Quorum restored → coordination resumes
**Given** coordination was suspended due to quorum loss
**When** quorum is restored (enough peers alive)
**Then** normal lease election resumes
**And** nodes campaign for roles
**And** new lease holders reconstruct state

### Requirement 4: State Synchronization Protocol

#### Scenario: New lease holder reconstructs state
**Given** a node acquires a coordination lease
**When** lease acquisition succeeds
**Then** the new holder performs state sync:
1. Reads local BoltDB for persisted state (`replication_state` bucket)
2. If local state missing/empty: queries peers via P2P RPC
3. Merges state (latest timestamp wins for `currentIndex`, `startTime`)
4. Validates state: `currentIndex` in bounds, `startTime` reasonable
5. Begins role duties with reconstructed state

#### Scenario: State sync via P2P RPC
**Given** new lease holder needs state from peers
**When** local BoltDB state missing
**Then** node sends `SyncStateRequest` to all alive peers via P2P
**And** peers respond with `SyncStateResponse` containing their state
**And** new holder merges responses (latest timestamp wins)
**And** `state_sync_timeout` (default 10s) bounds the sync

#### Scenario: State validation
**Given** reconstructed state from any source
**When** state is validated
**Then** the following checks pass:
- `currentIndex` in [0, len(replicationOrder)-1]
- `startTime` within reasonable range (not future, not > 24h old)
- `replicationOrder` is valid permutation of [1,2,3,4]
- If validation fails: log warning, initialize safe defaults (index 0, now)

### Requirement 5: Observability

#### Scenario: Leadership changes exported via metrics
**Given** a leadership change occurs
**When** role changes holder
**Then** the following metrics are updated:
```
momo_coordination_lease_holder{role="MetricsController"}     # Node ID of holder
momo_coordination_lease_holder{role="ReplicationPropagator"} # Node ID of holder
momo_coordination_leadership_changes_total{role="..."}       # Counter
momo_coordination_state_sync_duration_seconds                # Histogram
momo_coordination_quorum_status                              # 1=quorum, 0=no quorum
```

#### Scenario: Health-based resignation logged
**Given** node resigns due to health
**When** resignation completes
**Then** audit log: "Node X resigned MetricsController lease: CPU 95%, threshold 90%"

### Requirement 6: Configuration

```toml
[coordination_failover]
enabled = true
health_check_interval = "10s"
proactive_resign_threshold_cpu = 0.90
proactive_resign_threshold_mem = 0.90
proactive_resign_threshold_goroutines = 100000
split_brain_prevention = true
state_sync_timeout = "10s"
```

### Requirement 7: Backward Compatibility

#### Scenario: Failover disabled
**Given** `coordination_failover.enabled=false`
**When** daemon starts
**Then** failover features disabled:
- No SWIM-integrated lease revocation
- No proactive resignation
- No split-brain prevention (lease TTL only)
- Standard lease TTL expiry behavior
- State sync on acquisition still works (basic)

## Configuration Schema

```go
type ConfigurationCoordinationFailover struct {
	Enabled                     bool          `ini:"enabled"`
	HealthCheckInterval         time.Duration `ini:"health_check_interval"`
	ProactiveResignThresholdCPU float64       `ini:"proactive_resign_threshold_cpu"`
	ProactiveResignThresholdMem float64       `ini:"proactive_resign_threshold_mem"`
	ProactiveResignThresholdGoroutines int     `ini:"proactive_resign_threshold_goroutines"`
	SplitBrainPrevention        bool          `ini:"split_brain_prevention"`
	StateSyncTimeout            time.Duration `ini:"state_sync_timeout"`
}
```

## Integration Points

1. **LeaseManager** (`src/p2p/lease.go`): Extended with SWIM hooks, quorum checks
2. **Gossiper** (`src/p2p/gossip.go`): Publishes lease holder identity, revocation
3. **SWIM/PeerMap** (`src/p2p/peer_map.go`): Provides SUSPECT/OFFLINE events
4. **Coordinator** (`src/coordination/election.go`): Uses extended LeaseManager
5. **Metrics/Replication** (`src/metrics/metrics.go`, `src/server/replication.go`): Consume leadership changes

## Acceptance Criteria

- [ ] Lease holder failure detected in ~5s (SWIM suspicion) vs 30s (TTL)
- [ ] Proactive resignation triggers when health thresholds exceeded
- [ ] Split-brain prevented: no lease granted without quorum
- [ ] Quorum loss → coordination suspended, no split-brain
- [ ] New lease holder reconstructs state from BoltDB + peers within 10s
- [ ] State validation prevents corrupt state from causing issues
- [ ] Leadership changes visible in Prometheus metrics
- [ ] `coordination_failover.enabled=false` → standard lease behavior

## Failure Scenarios

### Network Partition
```
1. Partition splits cluster: Group A (3 nodes), Group B (2 nodes)
2. Group A has quorum (3 > 5/2), Group B no quorum
3. Group A: continues coordination, leases held
4. Group B: leases released, coordination suspended
5. Partition heals: Group B nodes rejoin, leases re-elected
```

### Lease Holder Crashes Without Warning
```
1. Node 1 holds lease, process crashes (no graceful resignation)
2. SWIM detects crash via timeout → marks OFFLINE
3. LeaseManager revokes lease immediately
5. Other nodes campaign for lease
6. New holder syncs state from BoltDB + peers
```

### State Corruption
```
1. New holder reads BoltDB → corrupted data
2. Queries peers for state
3. Peer state also corrupted
4. Validation fails → safe defaults (index 0, now)
5. Logs error for operator investigation
```

## Out of Scope

- Full consensus (Raft/Paxos) — lease + quorum sufficient
- Cross-region coordination (Phase 7)
- Automatic cluster expansion/shrinking (R11 rebalance)
- Per-tenant coordination roles (R8)