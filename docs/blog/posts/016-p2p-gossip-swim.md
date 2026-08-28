---
title: "P2P: Gossip, SWIM, and Membership"
date: 2026-03-11T14:34:18Z
draft: false
tags: [go, p2p, gossip, swim]
categories: [p2p]
summary: "A scaleless-gossip ring: SWIM-style failure detection, discovered-peer dialing, and ALIVE/OFFLINE restoration — momo's nervous system."
artifacts:
  - {type: spec, path: openspec/changes/add-p2p-transport}
  - {type: pr, id: "808"}
  - {type: pr, id: "809"}
  - {type: doc, path: docs/P2P.md}
related:
  - 004-cas-content-addressable-store
  - 017-scatter-gather-lease-quorum
  - 018-adaptive-scaling-peer-quality
---
# P2P: Gossip, SWIM, and Membership

Momo's cluster layer (`docs/P2P.md`) is a **masterless ring**: gossip for
metadata, SWIM failure detection, scatter-gather for list, and lease consensus
for its mutable metadata. This post covers the gossip/SWIM core; the lease/quorum
side is [017](017-scatter-gather-lease-quorum.md).

## SWIM: the failure-detection ring

- Nodes exchange **lifeheartbeat/ping/ack**, marking peers ALIVE/OFFLINE.
- **Discovered peers actually get connected** — an early audit (issue #598)
  caught that discovery produced peers nobody dialed (dynamic membership simply
  *didn't work*). Fixed: discovery → dial → peer map.
- **OFFLINE → ALIVE restoration** (#809): peers previously wedged in permanent
  death can return on ping/ack/heartbeat — the ring self-heals without restart.
- **Peer churn cleanup** (#808): closed connections and detached peers are
  removed from `PeerMap`/conns, ending a memory-leak-on-disconnect class.

## Why gossip, not a coordinator

Every node computes the same placement from CRUSH + membership — no leader can
evaluate "the config". Gossip is how membership (and thus CRUSH weights)
converges; scatter-gather and lease rosters ride on top
([017](017-scatter-gather-lease-quorum.md)).

## ⚡ Bolt lens

- `nextPingID` moved to `atomic.Uint64` (32-bit-safety, #896); `os.Hostname`
  cached once (metrics not per-scrape); peer RNG auto-seeded securely for
  unbiased shuffle (#897/#898).
- Gossip fanout later went **adaptive** with cluster size
  ([018](018-adaptive-scaling-peer-quality.md)) so a 100-node ring doesn't spam
  a 3-node one.

## Related

Leases + quorum: [017](017-scatter-gather-lease-quorum.md). Adaptive:
[018](018-adaptive-scaling-peer-quality.md).