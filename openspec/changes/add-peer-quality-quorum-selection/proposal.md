# Proposal: Peer-Quality-Aware Quorum Selection

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/823

- **Champion:** opencode (deepseek-v4-flash-free)
- **Status:** `Draft`

## 1. Problem

Momo's decentralized P2P layer runs quorum-based operations over the gossip
transport:

- **Scatter-Gather** queries (`src/p2p/scatter_gather.go`) broadcast to every
  alive peer,
- **Lease** consensus (`src/p2p/lease.go`) requests a majority from alive peers,
  and
- **Threshold-OPRF** evaluation (`src/p2p/oprf_rpc.go`) asks a quorum of alive
  peers for key shares.

Each of these currently selects its quorum with `Peers().Alive()` — which simply
returns **all** peers in the `PeerStateAlive` state with **no regard for peer
quality**. The gossip layer already tracks per-peer round-trip time (EWMA) in
`rttTracker` and liveness state, but quorum selection ignores it. Consequences:

- a slow, high-latency, or region-remote peer can be selected for a lease or
  OPRF quorum even when a fast, well-connected peer is available;
- scatter-gather wastes time contacting high-RTT/stale peers that are least
  likely to respond within the timeout window;
- this is inconsistent with the region-aware routing intent of Rule 41 (prefer
  low-cost members).

## 2. Proposed Solution

Make quorum selection quality-aware: prefer **live, low-RTT peers** and **exclude
suspect/offline peers** when assembling the quorum for scatter-gather, lease, and
OPRF evaluation.

### 2.1 Per-peer RTT tracking on `Peer`

Expose each peer's EWMA RTT directly on the `Peer` value so any consumer can
rank peers without reaching into the gossiper's private `rttTracker`:

- Add an atomic nanosecond RTT field on `Peer` (`src/p2p/types.go`), with
  `SetRTT(d time.Duration)` and `RTT() time.Duration` accessors.
- Have the gossiper's ping handler write its EWMA sample into the target peer
  (in addition to the existing `rttTracker.Update`), so `Peer.RTT()` reflects
  fresh ping data.

### 2.2 Quality-aware peer selection in `PeerMap`

Add `PeerMap.AliveByQuality() ([]*Peer)` returning **alive** peers (excluding
`Suspect` / `Offline`) sorted by RTT **ascending** (lowest RTT first = best
quality). Peers with unknown RTT (0) sort after known peers so freshly-joined
members are deprioritized until sampled, but never excluded while alive.

### 2.3 Apply to quorum consumers

Replace `Peers().Alive()` with `Peers().AliveByQuality()` in:

- `ScatterGather.Query` (`scatter_gather.go`),
- `LeaseManager.Acquire` (`lease.go`),
- `OPRFProvider.Evaluate` (`oprf_rpc.go`).

The quality-ordered list drives the same `Broadcast`/send path as today, so the
quorum is assembled from the best-available peers first; no alive peer is
dropped, but ordering prioritizes low-RTT members (and, by construction, excludes
offline/suspect peers that `Alive()` already filters).

## 3. Wire & Protocol Impact

None. No new RPC message types, payload fields, or byte layouts (Rules 7, 38).
Peer quality is a local, in-memory ranking used only to order/select the quorum.

## 4. Testing

- Unit tests: `Peer.SetRTT/RTT` round-trip; `AliveByQuality` ordering
  (low-RTT first, unknown last, suspect/offline excluded, all-alive preserved).
- Integration tests: three leading peers with distinct fake RTTs → quorum
  selection prefers the low-RTT peer first.
- Concurrency: `PeerMap.AliveByQuality` `-race` + `goleak.VerifyNone`.

## 5. Backward Compatibility

Fully compatible. `AliveByQuality()` returns the same alive set as `Alive()`,
only re-ordered; every current string test using `Alive()` semantics
(presence/exclusion) still holds, and no wire format changes. If all alive peers
have unknown RTT, ordering is stable and behavior matches today. Complies with
Rules 4 (no unbounded state — per-peer fixed field), 5 (tests), 7 (wire stable),
32 (bounded), 33 (feature works across transports), 41 (region/low-cost
preference).
