# Change: Coordinator Failover — SWIM-Based Leadership Election for Coordination Roles

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1147 (P1: SWIM-Based Leadership Election)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)
- Complements: `distributed-coordination-v1` (#1144)

## Why

The `distributed-coordination-v1` spec introduces lease-based election for coordination roles. This spec **completes the failover story** by implementing:

1. **Automatic failover** when lease holder fails (SWIM SUSPECT/OFFLINE)
2. **State transfer** during leadership handoff
3. **Split-brain prevention** via lease quorum
3. **Graceful degradation** when no quorum

Without this, the distributed coordination has a gap: if the lease holder dies, there's a window (up to lease TTL) where no coordinator acts.

## What Changes

### Phase 1: SWIM-Integrated Lease Failure Detection
- Extend `LeaseManager` to watch SWIM peer state
- On peer SUSPECT/OFFLINE: immediately trigger lease revocation check
- Reduce failover time from `lease TTL` to `SWIM suspicion timeout` (~5s vs 30s)

### Phase 2: Proactive Leadership Handoff
- Lease holder monitors own health (disk, memory, goroutine count)
- If health degrades: proactively resign lease before failure
- Graceful state transfer to next candidate

### Phase 3: Split-Brain Prevention
- Lease acquisition requires quorum (`(N/2)+1` alive peers)
- No lease granted without quorum → prevents split-brain
- If quorum lost: all roles released, cluster enters read-only coordination

### Phase 4: State Synchronization Protocol
- On leadership change: new holder fetches state from BoltDB + peers
- State includes: `currentIndex`, `startTime`, `replicationOrder`, `modeMetrics`
- State transfer via P2P RPC with timeout + retry

## Configuration

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

## Non-Goals

- Full Raft/Paxos consensus (lease-based sufficient for these roles)
- Cross-region coordination (Phase 7)
- Automatic cluster expansion/shrinking (R11)

## Impact

- **Failover time**: 30s (lease TTL) → 5s (SWIM suspicion)
- **Principles**: Self-Healing, Zero SPOF, Stigmergy
- **Operations**: Zero manual failover; proactive health-based resignation