# Change: Distributed Coordination — Metrics Controller + Replication Propagation

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1144 (P0: Distributed Metrics Controller + Replication Propagation)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

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

## What Changes

### Phase 1: Gossip-Based Leader Election
- Implement lease-based election for "coordination roles" using existing P2P lease infrastructure
- Two roles: `MetricsController` and `ReplicationPropagator`
- Any node can campaign for a role; lease holder performs the work
- Roles advertised via gossip (`GossipConfig` + `MetadataRPCProvider`)

### Phase 2: Distributed Metrics Controller
- All nodes run `GetMetrics` but only lease holder executes mode switching
- Non-lease-holders: monitor lease holder health via SWIM
- On lease holder failure: new election, new lease holder takes over
- State (currentIndex, start time) replicated via gossip or stored in BoltDB

### Phase 3: Distributed Replication Propagation
- Any node receiving `ChangeReplicationMode` RPC propagates to peers via gossip
- No designated "primary" — all nodes participate in propagation
- Deduplication via request IDs to avoid broadcast storms
- Timeout-bounded wait for peer acknowledgments

### Phase 4: Role Handoff + State Transfer
- On lease expiration/failure: new lease holder reconstructs state
- `currentIndex` and `start` time persisted in BoltDB (`replication_state` bucket)
- Graceful handoff: outgoing lease holder transfers state before release

## Configuration

```toml
[coordination]
enabled = true
metrics_controller_lease_ttl = "30s"        # Lease TTL for metrics controller
propagator_lease_ttl = "30s"                # Lease TTL for replication propagator
election_retry_interval = "5s"              # Retry interval when lease contested
state_persistence_interval = "10s"          # Persist state to BoltDB
```

## Non-Goals

- R6 distributed metadata (separate `r6-metadata-ha-v2`)
- R11 stigmergic rebalance (separate `r11-stigmergic-rebalance`)
- Full consensus (Raft/Paxos) — lease-based is sufficient for these roles

## Impact

- **Correctness**: No SPOF for coordination; cluster self-heals if node 0 dies
- **Principles**: Zero SPOF, Stigmergy, Read From Any Node
- **Operations**: No manual failover; automatic leadership transfer
- **Config**: New `[coordination]` section with safe defaults

## Risk Mitigation

- **Incremental**: Phase 1 (election) ships first; Phase 2-3 build on it
- **Backward compatible**: `coordination.enabled=false` → legacy node 0 behavior
- **Proven pattern**: Lease-based election used in etcd, Consul, Chubby
- **Reuse**: Leverages existing `LeaseManager`, `Gossiper`, BoltDB, SWIM