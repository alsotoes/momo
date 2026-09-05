---
title: "Replication Strategies and the Polymorphic Engine"
date: 2026-05-04T18:54:37Z
draft: false
tags: [go, replication, architecture]
categories: [origin]
summary: "Chain, Splay, and Primary-Splay replication — and the metrics-driven controller that switches between them at runtime."
artifacts:
  - {type: doc, path: docs/REPLICATION_STRATEGIES.md}
  - {type: spec, path: openspec/changes/add-replication-durability-floor}
related:
  - 001-origin-and-genesis
  - 003-transport-tcp-to-quic
  - 021-r3-write-durability-quorum
---
The original core of momo was four replication modes, chosen per object by a
**polymorphic controller** that watched CPU/memory and broadcast strategy
changes over a dedicated TCP control channel:

| Mode | Path | Transport sweet spot |
|---|---|---|
| `ReplicationNone` | no copies | ephemeral/test |
| `Chain` | N0 → N1 → N2 | high-bandwidth LAN |
| `Splay` | N0 → N1 **and** N2 | high-bandwidth LAN |
| `Primary-Splay` | client → all nodes concurrently | lossy/WAN, QUIC |

The design document (`docs/REPLICATION_STRATEGIES.md`) formalizes when each
applies and the downgrade rules the controller must respect — e.g. a
client-side replication fallback when the primary path degrades, later codified
in [`docs/EXTERNAL_CLIENT_REPLICATION.md`](../../EXTERNAL_CLIENT_REPLICATION.md).

## Why a polymorphic engine?

No single strategy is optimal: on a quiet LAN, **Chain** gives ordering and low
overhead; under WAN loss, **Primary-Splay** over QUIC avoids head-of-line
blocking; under memory pressure you want fewer, verified copies. Swapping at
runtime — rather than per cluster — makes the whole system a living experiment.

## ⚡ Bolt lens

The strategy switch itself is a decision point, not the data path. Replication
forwarding uses pooled buffers and bounded semaphores (later fixed for
context-awareness and blocking guards — see the durability floor arcs).
Performance-critical behavior was kept concrete; only the strategy selection is
polymorphic — the direct ancestor of **Rule 74 (Seam-Over-Plugins)**.

## Related

- [001: origin](001-origin-and-genesis.md)
- [003: TCP → QUIC transport](003-transport-tcp-to-quic.md)
- [021: R3 write durability and quorum](021-r3-write-durability-quorum.md)