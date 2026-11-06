# Change: R11 — Auto-Rebalance on Membership Change

**Related Issues:**
- https://github.com/alsotoes/momo/issues/939 (R11: Auto-rebalance on membership change)
- https://github.com/alsotoes/momo/issues/928 (Parent: Production readiness roadmap)

## Why

When a node joins or leaves the cluster, the CRUSH placement changes — some objects now map to different replica sets. Currently Momo has no automatic rebalancing: the new topology is used only for new writes, while existing data stays on old replicas. This leads to:

1. **Unbalanced load** — new node receives only new writes; old nodes keep all old data
2. **Under-replication** — if a node leaves, its replicas are not rebuilt elsewhere
3. **Over-replication** — if a node joins, old data isn't moved off overloaded nodes

## What Changes

### Membership Change Detection
- Leverage existing SWIM gossip: `PeerMap` already tracks `alive/suspect/offline` state
- New `RebalanceController` watches `PeerMap` for membership changes (join/leave/fail)
- On change: compute new CRUSH placement for all objects; generate rebalance plan

### Rebalance Plan Generation
- For each object (or shard in R6 distributed metadata):
  - Current replicas: `current = CRUSH(old_topology, object_hash, M)`
  - Target replicas: `target = CRUSH(new_topology, object_hash, M)`
  - `diff = symmetric_difference(current, target)`
  - If `len(diff) > 0` → object needs rebalance

### Rebalance Execution
- **Outbound** (source node sends to new replica):
  - Stream blob from local `BlobStore.GetBlob()` → `target_node.ReceiveRebalance(blob)`
  - Target verifies hash, stores, acknowledges
  - Source removes from local after quorum confirms
- **Inbound** (target node pulls from old replica):
  - `target_node.RequestRebalance(object_hash)` → old replica streams
- Coordinator ensures:
  - Max concurrent rebalances per node (`[rebalance] max_concurrent`, default `4`)
  - Bandwidth throttling (`[rebalance] max_bandwidth_mbps`, default `100`)
  - Checkpointing: resume from last completed object on restart

### Degraded Read During Rebalance
- Reads during rebalance: try current replicas first; if 404, try old replicas (from pre-rebalance placement)
- This ensures availability during rebalance without waiting for completion

### Config
- `[rebalance] enabled = true` (default)
- `[rebalance] max_concurrent = 4`
- `[rebalance] max_bandwidth_mbps = 100`
- `[rebalance] bandwidth_throttle = true`

## Non-Goals

- R6 distributed metadata rebalance (separate R6 metadata HA)
- Erasure-coded shard rebalance (future Phase 7)
- Cross-region rebalance (Phase 7 geo-distribution)

## Impact

- **Correctness:** Cluster converges to balanced state after any membership change
- **Availability:** Degraded reads ensure no read unavailability during rebalance
- **Operations:** Throttling prevents rebalance from impacting foreground traffic
- **Config:** New `[rebalance]` section in `momo.conf`