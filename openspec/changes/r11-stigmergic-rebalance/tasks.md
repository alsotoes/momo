# Tasks — R11: Auto-Rebalance via Stigmergic Local Rules

## Phase 1 — Core Infrastructure: Local Rule Engine
- [ ] Create `src/rebalance/controller.go` — local rule engine struct
  - `RebalanceController` with nodeID, store, peers, cmap, config
  - `Run(ctx)` — background goroutine with ticker
  - `runHomeostaticChecks()` — disk pressure, replication factor, domain spread
- [ ] Create `src/rebalance/shed.go` — blob shedding logic
  - `shedToPeer(peer)` — select cold blobs, stream with semaphore + bandwidth limiter
  - `selectColdBlobs(n)` — least accessed via access stats (or LRU)
  - `streamBlobToPeer(hash, peer)` — P2P transport with rate limiting
- [ ] Create `src/rebalance/replicate.go` — re-replication on leave
  - `OnPeerLeave(departedID)` — scan metadata, trigger re-replication
  - `reReplicateObject(obj)` — find survivor, select new target, stream
  - `selectSurvivor/replica` — from MetadataReplicas excluding departed
- [ ] Create `src/rebalance/domain.go` — failure domain spread check
  - `checkFailureDomainSpread()` — detect >1 replica in same domain
  - `migrateToDifferentDomain(obj)` — CRUSH target selection
- [ ] Config parsing: `[rebalance]` section in `config.go`
  - `enabled`, `interval`, `max_concurrent`, `max_bandwidth_mbps`
  - `disk_high_water`, `disk_low_water`, `request_rate_ratio`
- [ ] Unit tests: `rebalance_controller_test.go` (mock peers, verify rules)

## Phase 2 — SWIM Integration + Membership Events
- [ ] Wire `OnPeerJoin` / `OnPeerLeave` in `bootstrapP2P` (server.go)
  - `gossip.OnJoin` → `rc.OnPeerJoin(peer.ID)`
  - `gossip.OnLeave` → `rc.OnPeerLeave(peer.ID)`
- [ ] Implement `OnPeerJoin` in controller:
  - Recompute CRUSH for local objects
  - Identify objects where new peer is target
  - Trigger shed for those objects
- [ ] Implement `OnPeerLeave` in controller:
  - Find objects where departed was replica
  - Trigger re-replication for each
- [ ] Integration tests: 3-node cluster, add 4th node → verify shedding

## Phase 3 — Homeostatic Checks (Continuous)
- [ ] Disk pressure shedding:
  - `checkDiskPressure()` — local usage vs peer usage (via gossip)
  - `selectColdBlobs()` — use access stats or LRU approximation
  - Rate limiting: semaphore + token bucket bandwidth limiter
- [ ] Replication factor maintenance:
  - `checkReplicationFactor()` — scan metadata for under-replicated
  - Trigger re-replication with new CRUSH target
- [ ] Failure domain spread:
  - `checkFailureDomainSpread()` — analyze MetadataReplicas domains
  - Migrate one replica if concentrated
- [ ] Request rate redirection:
  - Extend existing proxy path: if peer has blob + lower load, redirect
- [ ] Integration tests: simulate disk pressure → verify shedding to peer

## Phase 4 — Degraded Read + Throttling + Polish
- [ ] Degraded read during rebalance:
  - If blob not found on current replicas, try pre-rebalance CRUSH targets
  - Update `getFile` / `DownloadWithFallback` to try old placement
- [ ] Bandwidth throttling:
  - Token bucket limiter in `shed.go` / `replicate.go`
  - Foreground traffic priority (separate semaphore or priority queue)
- [ ] Config validation + defaults in `config.go`
- [ ] Update `docs/GUIDES/CONFIGURATION.md`, `conf/momo.conf`
- [ ] Update `docs/ARCHITECTURE.md` with stigmergic rebalance section
- [ ] Blog post per Rule 76 (teach the stigmergic pattern)

## Phase 5 — Chaos Testing + Validation
- [ ] Chaos test: random join/leave/fail in 10-node cluster
  - Verify convergence to balanced state (disk usage, replica count)
  - Verify no data loss (all objects have RF replicas)
  - Verify read availability throughout (degraded read works)
- [ ] Benchstat: foreground throughput during rebalance
  - Target: <5% regression on client read/write
- [ ] Reviewer ✅ gate (Rule 55)

## Cross-Phase Requirements
- [ ] `go build ./...` clean at each phase
- [ ] `go test ./...` passes at each phase
- [ ] `go vet`, `gofmt -l` clean
- [ ] `go work vendor` parity (Rule 25)
- [ ] Blog post per Rule 76
- [ ] ADR sync (Rule 78) — new ADR for R11
- [ ] PR body with `Resolves #939`

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Chaos test: random membership changes → cluster converges
- Benchstat: no regression on client ops during rebalance
- Reviewer ✅ gate (Rule 55)