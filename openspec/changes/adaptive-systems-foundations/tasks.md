# Adaptive Systems Foundations (Ant-Colony Routing & Epigenetic Concurrency)

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1129

## A: Ant Colony Pheromone Routing

- [ ] A1. Define `PheromoneRouter` interface and `antColonyRouter` in `src/client/pheromone.go` with thread-safe atomic/mutex state.
- [ ] A2. Implement pheromone reward calculation on fast completion, severe penalty on failure, and exponential time-based evaporation (`tau *= 0.95`).
- [ ] A3. Implement roulette-wheel weighted replica selection over candidate nodes balancing exploration and exploitation.
- [ ] A4. Add unit and concurrency tests (`src/client/pheromone_test.go`) validating probability distribution, penalty backoff, race safety (`-race`), and goroutine leak freedom (`defer goleak.VerifyNone(t)`).
- [ ] A5. Benchmark `BenchmarkPheromoneSelectReplica` asserting zero heap allocations in the routing decision path (Rule 74).

## B: Resilient Client Fallback

- [ ] B1. Implement `client.DownloadWithFallback` in `src/client/client.go` integrating CRUSH placement and `PheromoneRouter`.
- [ ] B2. Loop through prioritized replicas: on connection/download failure, record penalty and attempt sibling replicas; on success, record reward.
- [ ] B3. Add table-driven integration tests in `src/client/client_test.go` simulating single-node down, multi-node failure, and corrupted payloads.

## C: Epigenetic Server Concurrency & Admission

- [ ] C1. Define `AdaptiveConcurrencyController` interface and `epigeneticController` in `src/server/concurrency.go`.
- [ ] C2. Implement hardware probing on boot: detect cgroups memory limits, file descriptor limits via `unix.Getrlimit`, and CPU cores.
- [ ] C3. Implement homeostatic admission control in `AcquireSlot` and `ReleaseSlot` with load-aware shedding.
- [ ] C4. Wire `AdaptiveConcurrencyController` into `server.go` accept loop, replacing static `const maxConcurrentConnections = 1000`.
- [ ] C5. Add unit and stress tests in `src/server/concurrency_test.go` with simulated resource pressure and `goleak.VerifyNone(t)`.

## D: Documentation Parity, ADR Sync & Verification

- [ ] D1. Synchronize ADR via `make adr-sync` (`docs/adr/0005-adaptive-systems-foundations.md`).
- [ ] D2. Update `docs/momofs/ADAPTIVE_SYSTEMS.md` reflecting implemented Phase 1 status for Principles 3, 6, and 10.
- [ ] D3. Verify full test suite: `go test -race ./src/client/... ./src/server/...` and `make test`.

## Steering-Rule Compliance Notes

- **Rule 2 & 12:** Decentralized Primary & CRUSH preserved; client dynamically queries CRUSH placement set without a central coordinator.
- **Rule 4 & 37:** Zero-Crash Pattern; every public seam method implements panic recovery returning formatted `syscall` errors.
- **Rule 5 & 40:** All tests run with `-race` and `goleak.VerifyNone(t)` on ephemeral test ports.
- **Rule 11 & 73:** Issue #1129 tracked, OpenSpec proposal and spec synchronized.
- **Rule 74:** Compile-time Go interface seams (`PheromoneRouter` and `AdaptiveConcurrencyController`); zero-alloc fast paths.
