> GitHub Issue URL: https://github.com/alsotoes/momo/issues/939

# Spec: R11 — Auto-Rebalance on Membership Change

## Requirements

### R11-M1: Membership Change Detection

**Requirement:** The system MUST detect cluster membership changes and trigger rebalance planning.

**Scenario: Node joins**

Given a new node joins the cluster (SWIM peer state `alive`),
When the membership change is detected,
Then `RebalanceController` generates a new rebalance plan for all objects.

**Scenario: Node leaves/fails**

Given a node transitions to `suspect` then `offline` (SWIM),
When the membership change is detected,
Then `RebalanceController` generates a rebalance plan for objects whose replicas included the departed node.

### R11-P1: Rebalance Plan Generation

**Requirement:** The rebalance plan MUST correctly identify objects needing movement.

**Scenario: Plan covers all affected objects**

Given old topology `T_old` and new topology `T_new` with replication factor `M`,
When `GenerateRebalancePlan()` runs,
Then for each object hash:
- `current = CRUSH(T_old, hash, M)`
- `target = CRUSH(T_new, hash, M)`
- If `symmetric_difference(current, target)` is non-empty, object is added to plan.

**Scenario: Plan stored persistently**

Given a generated plan,
When stored,
Then each item has: `object_hash`, `from_nodes`, `to_nodes`, `status` (pending/in_progress/completed/failed), timestamps.

### R11-X1: Rebalance Execution

**Requirement:** Blob transfer MUST verify integrity and respect R1 failure domains.

**Scenario: Outbound repair (source pushes to target)**

Given an object needs to move from source to target,
When `repairBlob` executes,
Then source streams blob via `BlobStore.GetBlob()` → target `ReceiveRebalance()`,
Target verifies SHA-256, stores, acknowledges,
Source removes local copy after quorum ack.

**Scenario: Failure-domain spread (R1)**

Given R1 is enabled with failure domains,
When `Restore` selects target nodes,
Then it prefers nodes in domains different from existing replicas.

**Scenario: Bounded concurrency + throttling**

Given `[rebalance] max_concurrent = 4` and `max_bandwidth_mbps = 100`,
When executing,
Then at most 4 concurrent repairs per node, token-bucket throttled to 100 Mbps.

### R11-D1: Degraded Read During Rebalance

**Requirement:** Reads MUST remain available during rebalance.

**Scenario: Degraded read falls back to old replicas**

Given an object is being rebalanced (source has it, target doesn't yet),
When a client reads the object,
Then the read tries current placement first; if 404, falls back to pre-rebalance placement.

### R11-C1: Checkpointing & Resume

**Requirement:** Rebalance MUST resume from interruption.

**Scenario: Restart mid-rebalance**

Given a rebalance is interrupted (node restart),
When the node restarts,
Then `RebalanceController` resumes from the last `in_progress` item.

## Non-Goals

- R6 distributed metadata rebalance (separate R6 metadata HA)
- Erasure-coded shard rebalance (future Phase 7)
- Cross-region rebalance (Phase 7 geo-distribution)