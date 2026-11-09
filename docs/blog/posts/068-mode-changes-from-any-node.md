---
title: "Mode Changes From Any Node"
date: "2026-11-09T18:40:00Z"
draft: false
post_type: architecture
tags:
  - coordination
  - replication
  - logical-clock
categories:
  - p2p
summary: "Only node 0 used to tell the cluster that the replication mode had changed. That is one more single point of failure than the design allows — and the fix hinges on a detail that is easy to get wrong: which timestamp decides whether an update is new."
artifacts: [{type: spec, path: openspec/changes/distributed-coordination-v1}, {type: adr, path: docs/adr/0065-distributed-coordination-v1.md}]
related: ["067-the-controller-that-left-node-zero", "065-metadata-writes-that-need-a-majority"]
difficulty: "advanced"
pattern: "logical-clock-guard"
teaches:
  - "Why a fixed propagation origin defeats decentralized coordination"
  - "How a logical timestamp stops gossip from looping"
  - "Why the handshake clock is the wrong clock for ordering"
---

A cluster can tolerate losing a node. It is much harder to tolerate losing the
only node allowed to say "we are changing replication mode now". Momo had exactly
that arrangement: whichever node received a mode change applied it, but only node
0 re-broadcast it to the rest of the cluster. If node 0 happened to be down, the
change landed in one process and stopped there.

## One Origin Is a Single Point of Failure

The previous design treated node 0 as the cluster's announcer. The reasoning was
simple — one broadcaster means no duplicate broadcasts — but it puts the control
plane back on a single machine after the metrics controller had just been moved
onto a lease. The asymmetry is also invisible in normal operation: with node 0
healthy, everything converges and nobody notices the assumption.

Removing the guard is easy. Making the removal safe is the interesting part.

## Why Not an LRU of Processed Messages?

The obvious anti-loop device is a cache: remember `mode:timestamp:origin` for
everything you have seen, and skip a message you already processed. It works, but
it is state that has to be sized, evicted and reasoned about — and it can be
wrong after a restart, which is exactly when a weak cache re-broadcasts a storm.

A logical timestamp needs no memory beyond the value it protects. The mode payload
already carries the time at which the change was created. Every node applies a
change only if that timestamp is newer than the one it last applied; a duplicate is
recognized by the timestamp alone and dropped. Convergence is a comparison.

## The Bug That Made It Obvious

The first implementation used the wrong clock. The receive path ordered updates by
the handshake timestamp — the value exchanged when a peer connects, checked for
freshness against replay. But every propagation hop dials a new connection, so
every hop carries a later handshake time. Each node therefore believed the message
it had just received was newer than the one it had just sent, applied it again,
persisted it again, and re-broadcast it again.

The result was a propagation storm: thousands of coordination state writes racing a
test teardown, which is how it was caught. The fix is one line of intent — order by
the timestamp in the payload, which is created once and carried unchanged across
hops — and one test that sends a handshake timestamp deliberately newer than the
payload, so a regression fails immediately instead of slowly.

That distinction is worth remembering beyond this codebase. A *connection* clock
answers "is this message fresh?". A *logical* clock answers "is this message new?".
Using the first for the second turns a benign retry into an infinite loop.

## What Convergence Now Looks Like

A node that applies a change persists it, audits it, and forwards it to the other
daemons. Peers that already hold that timestamp ignore the copy and simply
acknowledge it, so the sender is never left waiting. One change produces one round
of messages; the cluster agrees.

Local administrative overrides still apply unconditionally, and a zero timestamp
keeps the historical behaviour — an operator typing a command does not have to win
a race against the cluster's clock.

## References / Dig deeper

- OpenSpec change: `openspec/changes/distributed-coordination-v1/`
- Tracking issue: https://github.com/alsotoes/momo/issues/1144
- Lease election for the controller: post 067
