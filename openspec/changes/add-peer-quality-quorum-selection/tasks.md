# Peer-Quality-Aware Quorum Selection

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/823

## A: Per-peer RTT on Peer

- [x] A1. Add atomic nanosecond RTT field on `Peer` (`src/p2p/types.go`) with
  `SetRTT(time.Duration)` and `RTT() time.Duration` accessors.
- [x] A2. In `Gossiper.sendPing` / `handleAck`, after `rttTracker.Update`,
  write the EWMA sample to the target peer via `SetRTT`.
- [x] A3. Unit tests: `SetRTT`/`RTT` round-trip; concurrent `SetRTT`/`RTT`
  (`-race` + `goleak.VerifyNone`).

## B: Quality-ordered alive selection

- [x] B1. Add `PeerMap.AliveByQuality() ([]*Peer)` returning alive peers
  (excludes Suspect/Offline) sorted ascending by `RTT()`, unknown (0) last.
- [x] B2. Unit tests: low-RTT first; unknown last; suspect/offline excluded;
  all-alive preserved; concurrent calls race-free.

## C: Quorum consumers use quality ordering

- [x] C1. `ScatterGather.Query` (`scatter_gather.go`): use
  `Peers().AliveByQuality()` instead of `Alive()`.
- [x] C2. `LeaseManager.Acquire` (`lease.go`): use `AliveByQuality()`.
- [x] C3. `OPRFProvider.Evaluate` (`oprf_rpc.go`): use `AliveByQuality()`.
- [x] C4. Integration tests asserting low-RTT peer preferred in each quorum
  path over `net.Pipe`/`127.0.0.1:0` (Rule 40).

## D: Docs, Verification

- [x] D1. Docs parity (Rule 27): note quality-aware quorum selection in
  `docs/ARCHITECTURE.md` (and `docs/momofs/ADAPTIVE_SYSTEMS.md` if present).
- [x] D2. `go build ./...`, `go test -race ./...` (incl. `src/p2p` module),
  `go vet ./...`, `gofmt`.

## Steering-Rule Compliance Notes

- **Rules 4/32:** fixed per-peer field; no unbounded state.
- **Rules 5/40:** tests + race/goleak; ephemeral-port integration tests.
- **Rules 7/38:** zero wire changes.
- **Rule 33:** applied across all P2P transports (scatter-gather/lease/OPRF).
- **Rule 41:** low-cost (low-RTT) members preferred, region-aligned.
