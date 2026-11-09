---
title: "The Controller That Left Node Zero"
date: "2026-11-08T23:10:00Z"
draft: false
post_type: architecture
tags:
  - coordination
  - leases
  - failover
categories:
  - p2p
summary: "The component that decides when to change replication mode ran only on node 0 — a single point of failure in a system whose first principle is zero single points of failure. Election by lease fixes it without adding a coordinator: any node can hold the role, and the role can move."
artifacts: [{type: spec, path: openspec/changes/distributed-coordination-v1}, {type: adr, path: docs/adr/0065-distributed-coordination-v1.md}]
related: ["064-failing-over-coordination-in-seconds", "065-metadata-writes-that-need-a-majority"]
difficulty: "intermediate"
pattern: "leader-election"
teaches:
  - "Why a fixed coordinator role contradicts the design principles"
  - "How a lease elects a role without a election server"
  - "Why renewing a lease must not release it first"
---

A distributed storage system can survive losing a disk, a node, or a network link.
It is harder to survive losing a decision-maker. Momo had exactly one role still
pinned to a single machine: the loop that decides when the cluster should change
replication strategy. This is the story of moving that role onto a lease, so any
node can hold it and any node can inherit it.

## The Odd One Out

Momo's design principles start with "read from any node" and "zero single points
of failure". Placement is computed locally, metadata is sharded, and coordination
leases are already used for destructive operations. Then there was the
replication-mode controller: a control loop that watches CPU and memory and
decides when to move the cluster from one replication strategy to the next. It
ran on node 0. Only node 0. If node 0 died, the cluster kept serving reads and
writes — with whatever mode it happened to be in, forever.

That is a strange guarantee to lose over one small loop. The loop itself is not
special: it samples the host, compares against thresholds, and pushes a new mode
to peers. Any node can do it. What no node can do safely is do it at the same
time as another node.

## A Lease Is Enough

An election usually implies a quorum of voters, a term number, and a state
machine. Momo already had something simpler and sufficient: a lease manager that
asks a majority of peers to grant a time-bounded key. If a majority grants it,
you hold the key; if you stop renewing it, peers forget you and someone else can
take it.

So the role becomes a key. Every node campaigns for
`coordination/metrics-controller`. The holder runs the control loop; everybody
else stands by, checking on each tick whether they have become the holder. No
ballots, no terms, no new service to operate — just the same lease mechanism that
already protects deletes.

The subtle part is renewal. The obvious implementation — release the lease and
immediately re-acquire it — has a window where nobody holds the key, and two nodes
can end up believing they are the controller. Instead the holder sends the same
lease ID with a later expiry. Peers accept it because the ID matches the current
holder, and deny anybody else. Renewal extends exclusivity; it never hands it
over.

## What Happens When the Holder Dies

Nothing dramatic, which is the point. The holder stops renewing. After enough
missed renewals its lease expires on the peers that granted it. Meanwhile every
other node is campaigning on an interval and one of them now gets a majority.
That node reads the replication mode it last observed — every node receives every
mode update, and the mode is persisted locally — and announces it to the cluster.
The cluster converges on the mode it was already running rather than waiting for
the next threshold crossing.

## Keeping It Optional

Election changes the shape of the control loop, so it is opt-in: with
`[coordination] enabled=false` the legacy node-0 rule applies unchanged. That is
not just politeness to existing deployments. A single-node cluster cannot elect
anything, and an operator debugging a mode switch does not want a lease in the
story.

What remains is the other coordination role — replication propagation — which
still has a designated origin. Electing the controller was the part that could
lose a capability; propagation is the next piece of the same pattern.

## References / Dig deeper

- OpenSpec change: `openspec/changes/distributed-coordination-v1/`
- Tracking issue: https://github.com/alsotoes/momo/issues/1144
- Lease mechanics: `docs/REFERENCE/P2P.md`
