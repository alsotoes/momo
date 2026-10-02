---
title: "P2P: Gossip, SWIM, and Membership"
date: 2026-03-11T14:34:18Z
draft: false
post_type: architecture
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
  - 030-external-s3-client-replication-downgrade
  - 032-r5-metrics-phases-2-4
---
Momo's cluster has no leader. Nothing decides which machines exist, which are
healthy, or where data belongs. Every node runs the same membership logic and
converges on the same view of the ring — we call it a **masterless ring**. That
view has to survive nodes joining, dying, and returning, and keeping it correct
is the entire job of the gossip layer in this post.

Two ideas do the work. **Gossip** is a rumor mill: each node periodically swaps
small summaries with a random handful of peers, and any fact one node learns
spreads through the cluster without a central broadcaster. **SWIM** (Scalable
Weakly-consistent Infection-style Membership) is the failure-detection protocol
layered on top: a node pings a peer to check it is alive, and if the ping goes
unanswered it asks several *other* peers to ping on its behalf before declaring
the target dead. That indirect step is the trick — one congested link can no
longer produce a false death sentence.

{{< diagram src="/diagrams/06-gossip-swim.svg" alt="P2P Gossip and SWIM" caption="P2P Gossip and SWIM" >}}

## Failure detection, and the bugs that broke it

Nodes exchange heartbeats, pings, and acknowledgements, and mark each other
ALIVE or OFFLINE. The design is simple; the interesting part was everything the
first implementation got wrong.

- **Discovery that never dialed.** An early audit caught that the discovery step
  produced peers nobody actually connected to. Dynamic membership simply did not
  work: a new node could be "known" and still be unreachable. The fix wired the
  three steps together — discover, dial, then record the peer in the map.
- **Peers stuck dead forever.** A peer that had been marked OFFLINE stayed
  OFFLINE even after it recovered. The fix lets a ping, acknowledgement, or
  heartbeat revive a peer, so the ring self-heals without a restart.
- **Leaks on disconnect.** Closed connections and detached peers were never
  removed from the in-memory peer and connection maps, leaking memory every time
  a node left. The fix prunes both on disconnect.

## Why gossip instead of a coordinator

A coordinator would have to be authoritative, which means a leader election, a
single point of failure, and a node whose view of "the config" everyone else
trusts. Momo avoids that: every node computes data placement itself from the same
deterministic placement function plus the current membership list. The only thing
that has to converge is *membership*, and gossip is how it converges. Higher-level
operations — listing metadata, acquiring leases — ride on top of that converged
view rather than on a central server.

## Keeping it cheap

Membership runs on every node forever, so it is held to a tight allocation
budget. The ping counter became a 64-bit atomic, avoiding a 32-bit wraparound;
the hostname is cached once at startup instead of recomputed for every metrics
scrape; and the random peer selection is seeded from a secure source so the
shuffle is unbiased. Later, gossip fanout itself became adaptive to cluster size,
covered in [018](018-adaptive-scaling-peer-quality.md), so a hundred-node ring
does not flood a three-node lab cluster with traffic.

## References / Dig deeper

- Cluster design doc: [docs/P2P.md](../../P2P.md).
- Spec: `openspec/changes/add-p2p-transport`.
- Membership fixes: PR [#808](https://github.com/alsotoes/momo/pull/808),
  PR [#809](https://github.com/alsotoes/momo/pull/809); discovery audit issue
  [#598](https://github.com/alsotoes/momo/issues/598).
- Sibling posts: [017: Scatter-Gather and Lease Consensus](017-scatter-gather-lease-quorum.md),
  [018: Adaptive Scaling](018-adaptive-scaling-peer-quality.md).