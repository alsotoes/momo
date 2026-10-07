# Change: R11 — Auto-Rebalance via Stigmergic Local Rules (No Coordinator)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/939 (R11: Auto-rebalance on membership change)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

When a node joins or leaves the cluster, the CRUSH placement changes — some objects now map to different replica sets. Currently Momo has no automatic rebalancing: the new topology is used only for new writes, while existing data stays on old replicas. This leads to:

1. **Unbalanced load** — new node receives only new writes; old nodes keep all old data
2. **Under-replication** — if a node leaves, its replicas are not rebuilt elsewhere
3. **Over-replication** — if a node joins, old data isn't moved off overloaded nodes

**The original R11 spec proposed a `RebalanceController` — a central coordinator that computes plans and orchestrates moves. This violates core principles:**
- **Zero SPOF** — controller becomes a coordinator
- **Stigmergy** (ADAPTIVE_SYSTEMS.md) — complex global behavior must emerge from simple local rules, not a central plan
- **Adapt, don't configure** — the system should tune itself based on local conditions

## What Changes (Stigmergic Approach)

### Core Principle: Local Rules, Global Emergence

Each node runs **independent local rules** based on its own state and gossip from peers. No central controller. No global plan. The rebalance emerges from thousands of simple interactions.

```
Local Rules (each node runs these independently):

Rule 1 (Homeostasis — Disk Pressure):
  if my_disk_usage > 80% AND peer.disk_usage < 50%:
    shed least-accessed blobs to that peer
    
Rule 2 (Replication Factor Maintenance):
  if my_replica_count_for_object < target_RF:
    request re-replication from healthy replica
    
Rule 3 (Load Balancing — Request Rate):
  if my_request_rate > 2x peer.request_rate:
    redirect new requests to peer (via cache miss proxy)
    
Rule 4 (Membership Change — Join):
  if new_node_joined (via SWIM):
    shed some shards to new node (CRUSH says they now belong there)
    
Rule 5 (Membership Change — Leave):
  if peer_left (via SWIM):
    re-replicate data that was on that peer
    
Rule 6 (Failure Domain Spread):
  if my_replicas_in_same_domain > 1:
    migrate one replica to different domain
```

### Phase 1 — Membership Change Detection (Already Exists)
- Leverage existing SWIM gossip: `PeerMap` already tracks `alive/suspect/offline` state
- Each node independently detects join/leave via gossip — no central watcher needed

### Phase 2 — Local Shedding on Join (Stigmergic)
- When a node detects a new peer via gossip:
  - It recomputes CRUSH placement for its local objects
  - For objects where the new peer is now a target replica:
    - **Shed**: stream blob to new peer, update local metadata, ack
    - No coordination — each node decides independently what to shed
  - New peer receives shedded data, stores, becomes valid replica

### Phase 3 — Local Re-Replication on Leave (Stigmergic)
- When a node detects a peer leave via SWIM:
  - For each object where the departed peer was a replica:
    - **Re-replicate**: fetch from surviving replica, stream to new CRUSH target
    - Target selected by CRUSH on current topology
    - Surviving replicas independently initiate this (no coordinator)

### Phase 4 — Continuous Homeostatic Rebalance
- Background goroutine runs every `[rebalance] interval` (default 300s):
  - Checks local disk usage vs. peers (via gossip metrics)
  - If `my_disk > 80%` and peer `< 50%`: shed cold blobs to peer
  - If `my_replica_count < target_RF`: trigger re-replication
  - If replicas concentrated in one failure domain: migrate one

### Phase 5 — Degraded Read During Transition
- Reads during rebalance: try current replicas first; if 404, try old replicas (from pre-rebalance placement)
- This ensures availability during rebalance without waiting for completion

### Config
```toml
[rebalance]
enabled = true
interval = 300              # seconds between homeostatic checks
max_concurrent = 4          # max concurrent shed/re-replicate per node
max_bandwidth_mbps = 100    # bandwidth throttling
disk_high_water = 80        # % disk usage to trigger shedding
disk_low_water = 50         # % disk usage to accept shed
request_rate_ratio = 2.0    # my_rate / peer_rate to trigger redirect
```

## Non-Goals

- R6 distributed metadata rebalance (separate R6 metadata HA)
- Erasure-coded shard rebalance (future Phase 7)
- Cross-region rebalance (Phase 7 geo-distribution)
- Central coordinator or global rebalance plan

## Impact

- **Correctness:** Cluster converges to balanced state after any membership change
- **Availability:** Degraded reads ensure no read unavailability during rebalance
- **Operations:** Throttling prevents rebalance from impacting foreground traffic
- **Principles:** Zero SPOF (no coordinator), Stigmergy (local rules), Adapt-don't-configure

## Risk Mitigation

- **No coordinator** = no SPOF, no bottleneck
- **Proven pattern:** Ceph's PG rebalance is coordinator-based; this is more like Dynamo's hinted handoff + anti-entropy — fully decentralized
- **Reuse:** Leverages existing CRUSH, SWIM, gossip metrics, P2P streaming
- **Testing:** Chaos tests for join/leave/fail scenarios; verify convergence