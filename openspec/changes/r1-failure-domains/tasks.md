# Tasks: R1 — Failure-domain-aware CRUSH placement (#929)

## 1. Topology model (`src/common`)
- [ ] Add failure-domain representation to the node/cluster model (`ClusterMap`/`Node`)
- [ ] Parse `failure_domain` per `[daemon.N]` (optional; empty = unclassified)

## 2. Placement (`src/common/crush.go`)
- [ ] Implement domain-spread candidate selection (maximize distinct domains, tie-break
      `finalScore`) — REQ R1-C2
- [ ] Deterministic + minimal-movement behavior — R1-C3
- [ ] Degraded fallback with warning when domain count < R — R1-C4
- [ ] Keep `Placement` signature/protocol-compatible

## 3. Config
- [ ] Add `FailureDomain` to `ConfigurationDaemon` + load in `loadDaemonConfig`
- [ ] `conf/momo.conf` + `docs/CONFIGURATION.md` (Rule 27)

## 4. Tests (`src/common/crush_test.go`)
- [ ] R1-T1 same/multi/partial-domain optimum
- [ ] R1-T2 determinism
- [ ] R1-T3 degraded fallback warning
- [ ] R1-T4 goleak + `-race` green; benchmarks unchanged

## 5. Validation
- [ ] `go fmt`, `go vet`, `go build`, `go test` (common)
- [ ] `go work sync` + vendor parity
- [ ] Docs: `docs/CRUSH.md` updated (Rule 27)
