# Tasks — R11: Auto-Rebalance via Stigmergic Local Rules

## Phase 1 — Core Infrastructure: Local Rule Engine
- [x] Local rule engine — `src/server/rebalance.go` (`startRebalanceLoop` + `rebalanceCycle`), wired in `server.go`; no coordinator, no separate package (Rule 74 seam is the `rebalancePusher` interface)
  - `RebalanceController` with nodeID, store, peers, cmap, config
  - `Run(ctx)` — background goroutine with ticker
  - `runHomeostaticChecks()` — disk pressure, replication factor, domain spread
- [x] Blob shedding — `rebalanceCycle` picks coldest local blobs (modtime proxy), pushes to a placement peer, and drops local bytes only after the peer ack (`DaemonRebuildSource.Push`); `CASStore.ListBlobs`/`DropBlob`/`GetBlobByHash` + `LocalBlobStore.WalkBlobs`
  - `shedToPeer(peer)` — select cold blobs, stream with semaphore + bandwidth limiter
  - `selectColdBlobs(n)` — least accessed via access stats (or LRU)
  - `streamBlobToPeer(hash, peer)` — P2P transport with rate limiting
- [x] Re-replication on leave — covered by the R2 `rebuildLoop` (repairs any blob below target from a verified survivor); R11 adds no duplicate path
  - `OnPeerLeave(departedID)` — scan metadata, trigger re-replication
  - `reReplicateObject(obj)` — find survivor, select new target, stream
  - `selectSurvivor/replica` — from MetadataReplicas excluding departed
- [ ] Failure-domain migration — blocked by design: momo has no per-blob replica-location record to audit, so a concentrated replica set cannot be detected. R1 placement spreads new writes and R2 `Restore` prefers a different domain on repair; a real migration needs replica tracking first (see #1233 for the version-record analogue)
  - `checkFailureDomainSpread()` — detect >1 replica in same domain
  - `migrateToDifferentDomain(obj)` — CRUSH target selection
- [x] Config parsing: `[rebalance]` section (`enabled`, `interval`, `disk_high_water`, `disk_low_water`, `max_shed_per_cycle`) with validation and defaults
  - `enabled`, `interval`, `max_concurrent`, `max_bandwidth_mbps`
  - `disk_high_water`, `disk_low_water`, `request_rate_ratio`
- [x] Unit tests: storage blob listing/drop, config parsing/validation, cycle behavior (shed cap, below high water, push failure, no peer)

## Phase 2 — SWIM Integration + Membership Events
- [x] Wire `OnPeerJoin` in `bootstrapP2P` (`joinHandler` + `newJoinMigrator`); `OnPeerLeave` deliberately left to the R2 rebuild loop
  - `gossip.OnJoin` → `rc.OnPeerJoin(peer.ID)`
  - `gossip.OnLeave` → `rc.OnPeerLeave(peer.ID)`
- [x] Implement `OnPeerJoin` — `migrateToPeer` moves blobs the joining peer now owns and this node no longer owns
  - Recompute CRUSH for local objects
  - Identify objects where new peer is target
  - Trigger shed for those objects
- [x] Implement `OnPeerLeave` — intentionally omitted: the R2 rebuild loop re-replicates missing replicas
  - Find objects where departed was replica
  - Trigger re-replication for each
- [ ] Integration tests: 3-node cluster, add 4th node → verify shedding

## Phase 3 — Homeostatic Checks (Continuous)
- [x] Disk pressure shedding — `rebalanceCycle` (coldest-first, high/low water, per-cycle cap)
  - `checkDiskPressure()` — local usage vs peer usage (via gossip)
  - `selectColdBlobs()` — use access stats or LRU approximation
  - Rate limiting: semaphore + token bucket bandwidth limiter
- [x] Replication factor maintenance — covered by the R2 rebuild loop
  - `checkReplicationFactor()` — scan metadata for under-replicated
  - Trigger re-replication with new CRUSH target
- [ ] Failure domain spread — see the note above (needs replica tracking; new writes already spread by R1)
  - `checkFailureDomainSpread()` — analyze MetadataReplicas domains
  - Migrate one replica if concentrated
- [ ] Request rate redirection:
- [x] Rebalance observability — `momo_rebalance_{blobs_moved,bytes_moved,cycles,failures}_total` counters recorded on the push choke point
  - Extend existing proxy path: if peer has blob + lower load, redirect
- [x] Integration test: real TCP push through dial, handshake, metadata, streamed payload, ACK (`TestRebalancePushEndToEnd`); caught a stale handshake timestamp on the peer dial path

## Phase 4 — Degraded Read + Throttling + Polish
- [x] Degraded read during rebalance — R2 survivor-set read fallback, required before the loop will start
  - If blob not found on current replicas, try pre-rebalance CRUSH targets
  - Update `getFile` / `DownloadWithFallback` to try old placement
- [x] Streaming push — `BlobSize` + `pushReaderToPeer`/`writeThrottledReader`; no `io.ReadAll` on the rebalance path
- [x] Bandwidth throttling — `max_bandwidth_mbps` token bucket (`writeThrottled` + `bandwidthLimiter`) on every rebalance push; 0 = unlimited
  - Token bucket limiter in `shed.go` / `replicate.go`
  - Foreground traffic priority (separate semaphore or priority queue)
- [x] Config validation + defaults in `config.go` (`[rebalance]`)
- [x] Update `docs/GUIDES/CONFIGURATION.md`, `conf/momo.conf`
- [x] Update `docs/ARCHITECTURE.md` with the stigmergic rebalance section
- [x] Blog post 062 (Rule 76)

## Phase 5 — Chaos Testing + Validation
- [ ] Chaos test: random join/leave/fail in 10-node cluster
  - Verify convergence to balanced state (disk usage, replica count)
  - Verify no data loss (all objects have RF replicas)
  - Verify read availability throughout (degraded read works)
- [ ] Benchstat: foreground throughput during rebalance
  - Target: <5% regression on client read/write
- [x] Reviewer ✅ gate (Rule 55) — per PR

## Cross-Phase Requirements
- [x] `go build ./...` clean at each phase
- [x] `go test ./...` passes at each phase
- [x] `go vet`, `gofmt -l` clean
- [x] `go work vendor` parity (Rule 25)

- [x] ADR sync (Rule 78) — ADR 0072
- [x] PR body references the tracking issues without closing the umbrella (R11 is phased)

## Definition of Done (Per Phase)
- All phase tasks checked
- Integration tests pass on 3+ node cluster
- Chaos test: random membership changes → cluster converges
- Benchstat: no regression on client ops during rebalance
- Reviewer ✅ gate (Rule 55)