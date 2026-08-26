# Tasks: R1 — Failure-domain-aware CRUSH placement (#929)

## 1. Topology model (`src/common`)
- [x] Add failure-domain representation to the node/cluster model (`ClusterMap`/`Node`)
- [x] Parse `failure_domain` per `[daemon.N]` (optional; empty = unclassified)

## 2. Placement (`src/common/crush.go`)
- [x] Implement domain-spread candidate selection (maximize distinct domains, tie-break
      `finalScore`) — REQ R1-C2
- [x] Deterministic + minimal-movement behavior — R1-C3
- [x] Degraded fallback with warning when domain count < R — R1-C4
- [x] Keep `Placement` signature/protocol-compatible

## 3. Config
- [x] Add `FailureDomain` to `ConfigurationDaemon` + load in `loadDaemonConfig`
- [x] `conf/momo.conf` + `docs/CONFIGURATION.md` (Rule 27)

## 4. Tests (`src/common/crush_test.go`)
- [x] R1-T1 same/multi/partial-domain optimum
- [x] R1-T2 determinism
- [x] R1-T3 degraded fallback warning
- [x] R1-T4 goleak + `-race` green; benchmarks unchanged

## 5. Validation
- [x] `go fmt`, `go vet`, `go build`, `go test` (common)
- [x] `go work sync` + vendor parity
- [x] Docs: `docs/CRUSH.md` updated (Rule 27)
