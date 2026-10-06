---
title: "Adaptive Scaling: Gossip Fanout, Stream Chunks, Peer Quality"
date: 2026-08-14T19:43:24Z
draft: false
post_type: architecture
tags: [go, p2p, adaptive, peer-quality, bolt]
categories: [p2p]
summary: "Three adaptive feedback loops: gossip fanout scales with cluster size, streaming chunk size adapts to cipher/memory, peer quality feeds quorum decisions."
artifacts:
  - {type: pr, id: "833"}
  - {type: pr, id: "834"}
  - {type: pr, id: "835"}
  - {type: spec, path: openspec/changes/add-adaptive-gossip-scale}
  - {type: spec, path: openspec/changes/add-adaptive-streaming-chunk-size}
  - {type: spec, path: openspec/changes/add-peer-quality-quorum-selection}
related:
  - 016-p2p-gossip-swim
  - 017-scatter-gather-lease-quorum
  - 020-r2-degraded-read-self-heal
  - 030-external-s3-client-replication-downgrade
  - 044-plugin-seam-architecture
  - 052-adaptive-systems-foundations
---
Momo does not tune itself with a single knob. It grows three independent
feedback loops, each watching one signal and adjusting one decision at runtime,
so the cluster adapts to its own size, its own memory budget, and its own
slowest members. None of them change behavior for requests already in flight;
they change what the *next* request does.

## Gossip fanout scales with cluster size

**Gossip fanout** is how many peers a node contacts per gossip round. A fixed
number is wrong at both ends: pick it high enough for a large ring and a tiny lab
cluster drowns in redundant traffic; pick it low enough for the small cluster and
news takes too long to reach every node in a big one. The fix scales the fanout
target with a bounded function of node count — enough to spread information in a
hundred-node ring, capped so a three-node cluster stays quiet.

## Streaming cipher chunk size adapts

When momo streams encrypted data, the cipher has to frame the byte stream into
**chunks**, the units it encrypts and authenticates. Small chunks mean tighter
memory use and finer granularity, but more per-chunk framing overhead; large
chunks mean less overhead but bigger buffers. The chunk size is chosen at runtime
from the stream format version and the node's memory profile, keeping allocation
bounded and the on-the-wire semantics unchanged. The choice is a *seam*: a single
policy point that selects a compiled-in strategy rather than one that alters the
byte format itself.

## Peer quality feeds quorum selection

Lease voting, confidential deduplication (built on an oblivious pseudorandom
function, or OPRF), and scatter-gather all have to choose *which* peers to ask.
Instead of a one-rule policy like "fastest first", the system ranks candidates by
a quality signal, so slow or flaky peers lose quorum weight and reliable replicas
win. That ranking also feeds the survivor choice for degraded reads in
[020](020-r2-degraded-read-self-heal.md), so the node that answers a degraded read
is the one most likely to have good data.

## Keeping decisions off the byte path

Each adaptive loop is a *decision* point, deliberately separated from the flow of
bytes: measurement happens off the hot path, and the selection updates a
compile-time predicate. There is no runtime reflection and no dynamic plugin
loading — a declarative policy feeds a registry that was compiled in. This keeps
the adaptive machinery cheap enough to run on every node, and keeps its cost from
touching the request path. See [docs/STANDARDS.md](../../STANDARDS.md) for the
⚡ Bolt and 🛡 Sentinel mindsets behind that split.

## References / Dig deeper

- Adaptive gossip spec: `openspec/changes/add-adaptive-gossip-scale`;
  PR [#835](https://github.com/alsotoes/momo/pull/835).
- Adaptive streaming chunk spec:
  `openspec/changes/add-adaptive-streaming-chunk-size`;
  PR [#834](https://github.com/alsotoes/momo/pull/834).
- Peer-quality quorum spec:
  `openspec/changes/add-peer-quality-quorum-selection`;
  PR [#833](https://github.com/alsotoes/momo/pull/833).
- Sibling posts: [016: Gossip, SWIM, and Membership](016-p2p-gossip-swim.md),
  [017: Scatter-Gather and Lease Consensus](017-scatter-gather-lease-quorum.md),
  [020: Degraded Read Self-Heal](020-r2-degraded-read-self-heal.md).