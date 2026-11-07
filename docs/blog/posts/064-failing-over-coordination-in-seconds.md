---
title: "Failing Over Coordination in Seconds"
date: "2026-11-07T14:20:00Z"
draft: false
post_type: architecture
tags:
  - p2p
  - coordination
  - failover
categories:
  - p2p
summary: "A lease holder that only learns about a dead quorum when its lease expires keeps acting on stale authority for half a minute. This change connects membership suspicion to lease state, so coordination stops within seconds and a healthy node can take over without a coordinator."
artifacts:
  - type: spec
    path: openspec/changes/coordinator-failover-v1
  - type: adr
    path: docs/adr/0069-coordinator-failover-v1.md
related: ["063-gossip-load-without-a-coordinator", "062-stigmergic-disk-shedding"]
difficulty: "advanced"
pattern: "lease-failover"
teaches:
  - "Why a TTL alone is a slow safety net"
  - "How membership suspicion becomes a lease decision"
  - "Why a conservative quorum rule beats tracking granters"
---

One node in a cluster holds the right to make a decision — which replication mode
the cluster should use, who propagates it. It holds that right as a lease, with
an expiry. If the holder dies, the lease expires and someone else may take it.

That is a correct design with one uncomfortable property: between the moment the
cluster *notices* the holder is in trouble and the moment the lease expires,
nothing happens. With a thirty-second lease, the cluster spends up to thirty
seconds with a coordinator that cannot coordinate.

Everything needed to close that gap already exists. Membership gossip decides
within a few seconds that a peer is suspect and then offline. The lease layer and
the membership layer simply were not talking to each other.

## Two Kinds of Authority

There is a subtlety worth naming: a lease is not just a right, it is a
*dependency*. To acquire one, a node asks its peers for grants and needs a
majority. The node that holds the lease is therefore relying on the peers that
granted it — if they vanish, its authority is no longer backed by a quorum.

That dependency is what makes fast failover possible without introducing a
leader. When membership detects a degradation, the lease holder asks itself a
simple question: can I still satisfy the quorum I needed? If not, it stops.

## The Conservative Rule

A lease records how many grants it needed, not which peers gave them. Tracking
granters would be more precise — but it also means carrying, replicating and
reconciling a second dataset whose only purpose is to make a rare path slightly
more optimistic.

The rule used instead is deliberately blunt: count the currently reachable peers;
if that count is lower than the grants the lease required, release the lease.
Blunt in the safe direction. It may give up authority that would still have been
supportable, and a node that gives up too early merely loses a moment of
coordination; a node that keeps acting on a quorum that no longer exists can
split the cluster's decisions.

Releasing is itself a broadcast, so the other nodes learn immediately instead of
waiting for anything to expire.

## Where the Signal Comes From

Membership already has a single place where a peer transitions to suspect or
offline. That transition now notifies observers, and the lease manager is one of
them. Recovery back to alive is intentionally not a notification: coming back is
not a degradation, and a lease is re-acquired the normal way.

Two properties keep this safe in a hot path. The transition helper fires only on
an actual change, so repeated suspicion does not cascade into repeated lease
work. And every observer runs behind a panic guard — a bug in one listener must
not be able to take down gossip dissemination for the whole node.

## The Majority That Matters

Fast revocation answers "the peers I relied on are gone". It does not answer a
subtler question: how many nodes should count as a majority in the first place?

Counting a majority of *reachable* peers is circular — a minority partition
would happily elect itself, because in its own view it is the entire world. So
the cluster size is declared once at startup, and coordination requires a
majority of that configured set: three of five, two of three, one of one.

Two consequences follow naturally. Acquisition refuses to begin in a minority
partition instead of negotiating with whoever answers. And losing quorum releases
every held lease at once, not just the ones that are individually short of
grants — if the cluster can no longer form a majority, no node should be acting
as its coordinator.

The old reachable-peer heuristic remains only as a fallback for embedded and
single-process use, where no configured cluster exists to take a majority of.

## What Changes for an Operator

Failover time stops being "the lease TTL" and becomes "the suspicion timeout".
That is a meaningful difference during an incident: the window in which two nodes
might both believe they are the coordinator shrinks from tens of seconds to
seconds, and the cluster's decisions stop depending on a clock that was tuned for
something else.

The lease TTL still exists. It is now a backstop for the cases membership cannot
see, rather than the primary mechanism for noticing a failure.

## References / Dig deeper

- OpenSpec change: `openspec/changes/coordinator-failover-v1/`
- Tracking issue: https://github.com/alsotoes/momo/issues/1148
- Related post: publishing load without a coordinator (063)
