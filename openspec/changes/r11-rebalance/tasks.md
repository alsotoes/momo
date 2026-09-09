# Tasks: R11 — Auto-Rebalance on Membership Change

## Implementation

### Membership Change Detection
- [ ] `src/server/rebalance.go`: New file — `RebalanceController`
- [ ] `RebalanceController`: `Start()` — subscribes to `PeerMap` membership events (via `PeerMap.Subscribe()`)
- [ ] `RebalanceController`: On join/leave/fail → `GenerateRebalancePlan()`

### Rebalance Plan Generation
- [ ] `RebalanceController`: `GenerateRebalancePlan()`:
  - Iterate all objects (via `storage.Store.ListAllBlobs()` or R6 shard iteration)
  - For each: `current = CRUSH(old_members, hash, M)`, `target = CRUSH(new_members, hash, M)`
  - `diff = symmetric_diff(current, target)`; if non-empty → add to plan
- [ ] Plan stored in BoltDB `rebalance_plan` bucket: `object_hash → {from, to, status, started_at, completed_at}`

### Rebalance Execution
- [ ] `RebalanceController`: `ExecutePlan()` — processes plan items with concurrency control
- [ ] `RebalanceController`: `SendBlob(from, to, hash)` — streams `BlobStore.GetBlob()` to target's `ReceiveRebalance`
- [ ] `src/server/rebalance.go`: Add `ReceiveRebalance(hash, reader)` handler on server (new RPC endpoint)
- [ ] Target: verify hash (SHA-256), store via `BlobStore.PutBlob()`, ack
- [ ] Source: after quorum ack, `BlobStore.DeleteBlob()` from local
- [ ] Concurrency: semaphore `[rebalance] max_concurrent` (default 4)
- [ ] Throttling: token bucket per node `[rebalance] max_bandwidth_mbps` (default 100)

### Degraded Read
- [ ] `src/storage/storage.go`: `GetBlobDegraded(hash)` — tries current replicas, falls back to old placement
- [ ] `src/transport/s3_communicator.go`: `handleGetObject` uses `GetBlobDegraded` during rebalance
- [ ] `src/transport/momo_tcp.go` / `momo_quic.go`: native protocol uses `GetBlobDegraded`

### Checkpointing / Resume
- [ ] Plan items have `status` (pending/in_progress/completed/failed)
- [ ] On restart, `RebalanceController` resumes from last `in_progress` item

### Config
- [ ] `src/common/config.go`: Parse `[rebalance]` section
- [ ] `conf/momo.conf`: Document `[rebalance]` keys

## Testing

- [ ] `TestRebalancePlanGeneration` — join/leave triggers correct plan
- [ ] `TestRebalanceExecution` — single object moves from A to B; hash verified
- [ ] `TestDegradedRead` — during rebalance, GET succeeds from old replica
- [ ] `TestConcurrencyThrottle` — max 4 concurrent; bandwidth limited
- [ ] `TestCheckpointResume` — restart mid-rebalance; resumes from last item
- [ ] `go test -race ./...` — all tests pass
- [ ] `make test` — full suite passes

## Compliance

- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76)
- [ ] PR body includes `Resolves #939` (Rule 11)