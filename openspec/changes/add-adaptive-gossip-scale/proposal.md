# Proposal: Adaptive Gossip Fanout Scaling with Cluster Size

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/825

- **Champion:** opencode (deepseek-v4-flash-free)
- **Status:** `Draft`

## 1. Problem

Momo's gossip layer (`src/p2p/gossip.go`) uses a **fixed** fanout
(`DefaultGossipConfig.Fanout = 3`, overridable via `[p2p] fanout`). The number
of peers a node gossips to per heartbeat does not scale with cluster size:

- On a **small** cluster (e.g. 2 nodes), a fanout of 3 wastes the extra two
  sends (there are only 1-2 eligible peers) and adds redundant RTT probes.
- On a **large** cluster (e.g. 50 nodes), a fanout of 3 provides slow gossip
  convergence — information (membership joins/leaves, tombstones) can take many
  rounds to propagate.

Gossip literature recommends a fanout proportional to `ln N` to bound message
amplification while keeping convergence time bounded.

## 2. Proposed Solution

Make gossip fanout **adaptive to current cluster size**: default to
`ceil(ln N)` where `N` is the number of alive peers the node currently knows,
bounded to a minimum and maximum, while respecting an explicit configured
`fanout > 0` as an override.

### 2.1 Adaptive fanout function

Add `adaptiveFanout(aliveCount int) int` in `src/p2p`, computed as
`clamp(ceil(ln(N)), minFanout, maxFanout)` with defaults:
- `minFanout = 1` (always gossip to at least one peer when any exist),
- `maxFanout = 10` (bounded to avoid a single node overloading the cluster,
  Rule 4/32),
- effectively `max(1, min(10, ceil(ln(N))))`.

For example: `N=2 → 1`, `N=7 → 2`, `N=20 → 3`, `N=55 → 4`.

### 2.2 Config semantics

- `[p2p] fanout = 0` (or unset) → **adaptive** (new default): use
  `adaptiveFanout(aliveCount)` per heartbeat.
- `[p2p] fanout > 0` → fixed override, unchanged behavior (backward
  compatible). Existing configs that explicitly set fanout keep it.

### 2.3 Where fanout is applied

In `Gossiper.sendHeartbeat`, resolve the effective fanout from the current
`Peers().Alive()` count: if `cfg.Fanout > 0` use it, else `adaptiveFanout(count)`.
The same resolution applies to the indirect-ping fanout in `sendIndirectPing`
(which uses `IndirectPingCount`, left as-is) — note indirect-ping count is
separate and unchanged.

## 3. Wire & Protocol Impact

None. Heartbeat RPC message types and payloads are unchanged (Rules 7, 38).
Fanout is purely a local send-policy decision (how many peers to pick per round).

## 4. Testing

- Unit tests for `adaptiveFanout`: small cluster → min, large cluster → capped
  at max, monotonic scaling, clamping bounds.
- Unit test for fanout resolution: explicit `fanout=0` → adaptive; `fanout>0` →
  fixed.
- Integration: a 3-node cluster using default (adaptive) config still converges
  (gossip tests use `Fanout: 3`, so verify the default path separately).
- Concurrency: `-race` + `goleak.VerifyNone`.

## 5. Backward Compatibility

- Explicit `fanout > 0` is preserved verbatim.
- Default (no `fanout`) now scales as `ceil(ln N)` instead of a flat 3. On
  small clusters this is ≤ 3 (no regression), and on large clusters it improves
  convergence. No existing config that sets `fanout` is affected. Complies with
  Rules 4/32 (bounded fanout), 5 (tests), 7 (wire stable), 33 (applies across
  all P2P transports), 41 (region/low-cost), 46 (no pipeline file changes).
