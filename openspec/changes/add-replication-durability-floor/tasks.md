# Replication Durability Floor Under Load

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/822

## A: Durability computation + config

- [x] A1. Add `effectiveModeDurability(mode, replicationFactor, clusterSize) int`
  helper (in `src/metrics`): `ReplicationNone` → 1; replicated modes →
  `min(replicationFactor, clusterSize)`.
- [x] A2. Add `MinimumDurabilityFactor int` to `ConfigurationGlobal`
  (`src/common/struct.go`).
- [x] A3. Parse `[global] minimum_durability_factor` in `loadGlobalConfig`
  (`src/common/config.go`); default `0`; validate `< 0` and
  `> replication_factor` (when > 0) → `EINVAL`.
- [x] A4. Unit tests for `effectiveModeDurability`.

## B: Controller enforcement

- [x] B1. In `GetMetrics` (`src/metrics/metrics.go`), add a guard helper
  `shouldSwitchMode(cfg, currentIdx, newIdx, replicationOrder)` that blocks a
  degrade when the new mode's durability < floor; escalation always allowed.
- [x] B2. Apply the guard to the `checkMetricsAndSwap` change branch and the
  timeout-fallback degrade branch; log a warning when held (Rule 9).
- [x] B3. Only engage when `cfg.Global.MinimumDurabilityFactor > 0`.

## C: Tests, Docs, Verification

- [x] C1. Unit tests: degrade blocked below floor, escalation allowed, floor=0
  no-op, timeout-fallback held at floor.
- [x] C2. Config tests: default 0, parsed positive, negative / too-large
  rejected.
- [x] C3. Docs parity (Rule 27): `docs/CONFIGURATION.md`
  (`minimum_durability_factor`); note in `docs/ARCHITECTURE.md` how the
  controller holds at the floor.
- [x] C4. `go build ./...`, `go test -race ./...`, `go vet ./...`, `gofmt`.

## Steering-Rule Compliance Notes

- **Rules 4:** no new unbounded state.
- **Rules 5:** tests for all new logic.
- **Rules 7/38:** wire protocol unchanged.
- **Rule 9:** degrade held at the floor is logged, not silent.
- **Rule 27:** config + architecture doc parity.
