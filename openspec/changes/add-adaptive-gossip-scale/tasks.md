# Adaptive Gossip Fanout Scaling with Cluster Size

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/825

## A: Adaptive fanout computation

- [x] A1. Add `adaptiveFanout(aliveCount int) int` in `src/p2p/gossip.go`:
  `clamp(ceil(ln(N)), minFanout=1, maxFanout=10)`.
- [x] A2. Add `minGossipFanout`/`maxGossipFanout` constants.
- [x] A3. Unit tests: min at small N, cap at large N, monotonic, bounds.

## B: Config + resolution

- [x] B1. Change `[p2p] fanout` default to `0` (= adaptive) in
  `loadP2PConfig` (`src/common/config.go`); keep explicit `> 0` as override.
- [x] B2. Add `effectiveFanout(cfgFanout, aliveCount int) int`: `cfgFanout > 0`
  → cfgFanout; else `adaptiveFanout(aliveCount)`.
- [x] B3. Use `effectiveFanout` in `Gossiper.sendHeartbeat`
  (`src/p2p/gossip.go`) with `len(Peers().Alive())`.
- [x] B4. Unit tests for `effectiveFanout` (0 → adaptive, >0 → fixed) and a
  send-time resolution test.

## C: Tests, Docs, Verification

- [x] C1. 3-node integration test using default (unset) fanout still converges.
- [x] C2. Docs parity (Rule 27): `docs/CONFIGURATION.md` (`fanout` semantics),
  `docs/P2P.md` (adaptive fanout note).
- [x] C3. `go build ./...`, `go test -race ./...` (incl. `src/p2p`),
  `go vet ./...`, `gofmt`.

## Steering-Rule Compliance Notes

- **Rules 4/32:** fanout bounded `[1, 10]`.
- **Rules 5:** tests for computation, resolution, integration.
- **Rules 7/38:** zero wire changes.
- **Rule 33:** applies across all P2P transports (gossip fanout is transport-
  agnostic).
- **Rule 46:** no pipeline/reviewer file changes.
