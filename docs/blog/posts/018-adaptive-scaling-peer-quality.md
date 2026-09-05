---
title: "Adaptive Scaling: Gossip Fanout, Stream Chunks, Peer Quality"
date: 2026-08-14T19:43:24Z
draft: false
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
---
Momo doesn't tune itself with one knob — it grows three independent feedback
loops that learn the cluster shape at runtime.

## Gossip fanout scales with cluster size (#835)

Fixed fanout either floods small rings or starves large ones. Fanout target now
scales with a (bounded) function of node count — a 100-node ring gossip ring
doesn't drown a 3-node lab cluster. Ratified in
`openspec/changes/add-adaptive-gossip-scale/`.

## Streaming cipher chunk size adapts (#834)

Streaming E2EE encryption has a framing choice: small chunks = tighter memory +
finer granularity but more framing overhead. The cipher chunk size adapts to
`StreamVersion`/memory profile at runtime (bounded allocation, stable semantics
— Rule 74 era "seam" discipline). See
`openspec/changes/add-adaptive-streaming-chunk-size/`.

## Peer quality feeds quorum selection (#833)

Lease/OPRF/scatter-gather picks ranked peers by **quality signal**, not faster
one-rule policies: reorder candidates by a quality metric so slow/flaky peers
lose quorum weight and the system self-self selects reliable replicas. Directly
feeds the degraded-read survivor choice in [020](020-r2-degraded-read-self-heal.md)
for R2. See `openspec/changes/add-peer-quality-quorum-selection/`.

## ⚡ Bolt lens

Each adaptive loop is a *decision* point, dispatched away from the byte flow
(**Rule 74**): measurement happens off the hot path, and selection updates a
compile-time predicate. No runtime reflection, no plugins — a declarative
policy feeding a compiled-in registry.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Gossip: [016](016-p2p-gossip-swim.md). Leases/quorum: [017](017-scatter-gather-lease-quorum.md).
Resilience: [020](020-r2-degraded-read-self-heal.md).