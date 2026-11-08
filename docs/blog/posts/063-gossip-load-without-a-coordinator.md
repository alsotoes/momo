---
title: "Publishing Load Without a Coordinator"
date: "2026-11-07T12:00:00Z"
draft: false
post_type: architecture
tags:
  - p2p
  - adaptive
  - r11
categories:
  - p2p
summary: "A cluster that never looks at its neighbours will keep sending work to a node that is already full, but asking a central broker multiplies traffic exactly when the cluster is under stress. This change has each node publish a fixed twenty-byte load summary on the gossip channel it already runs, so every node can read peer load from memory instead of a query."
artifacts:
  - type: spec
    path: openspec/changes/r11-gossip-load-signal
  - type: adr
    path: docs/adr/0088-r11-gossip-load-signal.md
related: ["062-stigmergic-disk-shedding", "061-cross-node-version-records", "064-failing-over-coordination-in-seconds", "065-metadata-writes-that-need-a-majority", "066-thresholds-that-learn"]
difficulty: "intermediate"
pattern: "gossip"
teaches:
  - "Why observation is cheaper than per-request queries"
  - "How to keep a gossip payload bounded and validated"
  - "Why the decision half needs its own contract"
---

A storage cluster that never looks at how busy its neighbours are cannot help
but make one kind of mistake: it keeps sending work to a node that is already
overloaded. The obvious fix — ask the cluster who has capacity — is also the
obvious trap. A per-request query multiplies traffic exactly when the cluster is
under stress, and a central load broker turns every request into a dependency on
one more service.

So instead of asking, the nodes tell each other. Quietly, on a channel they
already share.

## The Information Gap

Membership gossip in this system already answers "who is reachable?" and "how far
away are they?" It does not answer "who has room?" Those are different
questions, and the second one changes second by second.

Without an answer, a node has one honest option: shed. Move cold bytes off the
full node so it stops being full. That works for capacity, but it cannot help a
node that is full because of *requests* rather than data.

## Ride the Channel You Have

Gossip already runs a periodic heartbeat to a small random slice of peers. Each
node now attaches one extra message to that same fanout: a twenty-byte summary of
disk utilisation, in-flight requests, and a sample timestamp.

Three properties make this affordable:

- **Fixed size.** Twenty bytes, validated on receipt. A peer cannot claim 400%
  disk; the decoder rejects it. Bounded payloads mean gossip cost cannot grow
  with cluster activity.
- **No new cadence.** The report travels with the heartbeat, so the cluster's
  background chatter does not double.
- **No new failure mode.** If a report never arrives, the peer simply has no
  load view. Nothing waits on it, and nothing breaks.

The receiving side keeps only the latest report per peer, stored atomically, so
reading a peer's load is a pointer load rather than a lock or a network call.

## Making It Visible First

A signal nobody can see is a signal nobody trusts. Before any node acts on peer
load, the reports are exported as metrics: one gauge per peer for disk
utilisation, and one for in-flight requests, both labelled by peer ID.

The shape matters. These are per-peer samples, not a cluster total, because
averaging utilisation across nodes hides exactly the case that matters — one
node pinned at 99% while the rest idle. And the families are declared even when
no reports exist yet, so a dashboard can distinguish "no data" from "a healthy
zero".

Exporting first also gives operators a chance to sanity-check the numbers
against reality before the same numbers are allowed to influence where traffic
goes.

## What This Is Not

This is not a redirect rule, and it deliberately does not become one yet.
Deciding to send a client elsewhere requires a contract: for an S3-compatible
gateway that means a redirect response that real clients follow, and for the
native protocol it means honest advisory metadata rather than a surprise
handoff. Getting that wrong loses writes, and a load signal is not worth a lost
write.

What ships today is the observation half. Every node can now answer "how loaded
are my peers?" from memory. The decision half has a foundation to stand on
instead of a guess.

## Why Not Just One Node Deciding?

A single load-aware coordinator is simpler to write and much worse to operate.
It becomes a dependency for every admission decision, it needs its own
availability story, and its view of the cluster is only as fresh as its last
poll. Local observation trades a small amount of staleness for the absence of a
new single point of failure — the same trade the placement and metadata layers
already make.

## References / Dig deeper

- OpenSpec change: `openspec/changes/r11-gossip-load-signal/`
- Follow-up (redirect contract): https://github.com/alsotoes/momo/issues/1261
- Shedding design and safety invariants: `docs/ARCHITECTURE.md`, section 4g-bis
- Related post: stigmergic disk shedding (062)
