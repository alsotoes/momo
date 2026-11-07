---
title: "Shedding Load Without a Coordinator"
date: "2026-11-07T03:09:12Z"
draft: false
post_type: architecture
tags:
  - r11
  - rebalance
  - storage
categories:
  - storage
summary: "A cluster that only ever writes leaves every node holding data it no longer has room for, and the usual fix — a rebalance coordinator — reintroduces the single point of failure the storage layer was designed to avoid. This change gives each node a local rule instead: when its disk crosses a high-water mark, it moves its coldest blobs to a peer that already holds a replica, and only drops the local bytes after that peer confirms them."
artifacts:
  - type: spec
    path: openspec/changes/r11-stigmergic-rebalance
  - type: adr
    path: docs/adr/0072-r11-stigmergic-rebalance.md
related: ["061-cross-node-version-records"]
difficulty: "intermediate"
pattern: "stigmergy"
teaches:
  - "Why a local high-water rule beats a rebalance coordinator"
  - "How to shed bytes without ever risking the last copy"
  - "Why the coldest blob is the right first mover"
---

## The Real Problem

Storage clusters fill unevenly. A node that took more than its share of writes
reaches its disk limit while its peers sit half empty, and from then on every
write it receives fails — even though the cluster as a whole has room.

The instinctive fix is a rebalance coordinator: one process that watches the
cluster and tells nodes what to move. That is the wrong shape for this system.
Momo's storage layer is built so that any node can serve any object and no
single process is required for correctness. Introducing a coordinator to keep
the disks even would hand a coordinator-shaped failure back to a system that was
designed not to have one — and if the coordinator dies, disks keep filling with
nobody watching.

## A Rule, Not a Plan

Instead of a central plan, each node carries one local rule:

> If my disk is above the high-water mark, move my coldest blob to a peer that
> already holds a copy — and only after that peer confirms it has the bytes,
> drop my copy.

That is all. There is no global view, no negotiation, and no leader. The
behavior of the cluster is the sum of every node applying the same rule to its
own state. This is stigmergy: local agents reacting to local conditions produce
a coherent global outcome, the same way ants leave and follow traces instead of
sharing a map.

The rule has three properties that make it safe to run unattended.

**The peer already holds a copy.** The target is not an arbitrary healthy node;
it is a node in the blob's placement set. Shedding therefore does not add a
replica or change durability — it just moves bytes to a node that was already
supposed to have them. If placement says the blob belongs on that peer anyway,
the cluster gets strictly more even.

**Confirmation before deletion.** The push is a real protocol exchange: metadata,
payload, acknowledgement. Local bytes are removed only after that
acknowledgement. A node that cannot reach a peer keeps its data and tries again
on the next tick. Damage requires two independent failures at the same moment.

**Reads survive the move.** Metadata is not part of the shed. The object still
resolves to the same content hash; only the bytes moved. A local read on the
now-lighter node falls back to the survivor path, fetches a verified copy from
the peer, and repairs itself. That is why the loop refuses to start unless
degraded read is enabled — shedding without that fallback would turn a
housekeeping action into an outage.

## Choosing What to Move

The cheapest useful signal for "cold" is time. The node sorts its blobs by
modification time and starts with the oldest — the bytes that have gone longest
without being written. It is an approximation: a read-heavy object written once
a year ago may be hot. But it is free, it needs no new bookkeeping, and it
degrades gracefully — if the guess is wrong, the object is fetched again from
its replica and the cost is one extra network read, not data loss.

The loop keeps its own budget. It stops when disk usage falls below the
low-water mark, and it moves at most a configured number of blobs per pass. That
second bound matters: a node that is very full should recover over several
cycles, not by saturating its uplink for ten minutes while foreground traffic
starves.

## Staying Out of the Way

A rebalance that competes with user traffic is not a rebalance; it is an outage
with extra steps. Shedding a large blob is exactly the moment when a node is
busy and its uplink is precious, so the rule needs a brake.

The brake is a token bucket: `max_bandwidth_mbps` sets how many bytes per second
the rebalance path may spend, refilled continuously, and each 64 KiB write must
pay for itself before it goes out. Foreground requests keep their own path and
are never blocked behind a shed. When the cap is zero, the bucket is removed and
the old behavior returns — useful for labs where the rebalance link is dedicated.

Two details matter in practice. The bucket is per push attempt, not global, so a
failed transfer does not permanently eat the budget. And the budget is spent
*before* the write, not after, which is what actually protects the socket queue
from a burst of buffered blobs.

## Shedding Without Buffering

There is a subtle trap in moving bytes: the obvious implementation reads the
blob into memory and then writes it out. For a small object that is invisible.
For a multi-gigabyte archive it means the node that is already under disk
pressure also spikes its memory — the wrong resource at the wrong moment.

The size of a blob is already known from its directory entry, so the rebalance
push reports that size to the peer and streams the file straight from disk
through the throttling writer. No intermediate copy, no `readAll`, no memory
proportional to object size. The peer learns the exact length up front, which
also keeps the transfer framing honest.

Streaming does give up one thing: the whole-payload checks the buffered path
could do before sending. That check is not lost — every node verifies the
content hash on ingest and on read, so a corrupted stream is rejected where it
lands rather than trusted because it was checked at the source.

## Can You Tell It Is Working?

A background process that moves data and says nothing is impossible to operate:
when disks stay full you cannot tell whether the rule is misfiring, whether the
pushes are failing, or whether the loop is not running at all.

So the path is instrumented at the choke point — the push — not at the decision.
Each successful push adds one to a moved-blob counter and its byte count; each
failure adds to a failure counter; each tick adds to a cycles counter. Because
the counters sit on the transport step, they measure bytes that actually left
the node rather than blobs the loop intended to move, which is the number that
matters when you are diagnosing a stuck drain.

All four are plain monotonic counters, so a rate over time answers the useful
questions: bytes per second tells you whether the throttle is binding, failures
per minute tells you whether a peer is unreachable, and a flat cycles counter
tells you the loop is not running even though the config says it should be.

## Fail at Startup, Not at 3 a.m.

A background rule that cannot run should say so immediately. If shedding is
enabled but the node is alone, or replication is set to one, or degraded read is
off, the loop has nothing safe to do — and the worst outcome is silence: disks
fill while the config claims a rebalance is enabled.

So the preconditions are checked when the configuration loads, and any violation
aborts startup with a message that names the missing piece. The three that
matter are structural, not tunable: shedding needs a peer to shed to
(`replication_factor >= 2`, at least two daemons), it needs the survivor read
path (`degraded_read`), and it needs cluster membership (`p2p`).

This is a small piece of code carrying a large operational rule: a feature that
can be silently inert is a feature you cannot trust during an incident.

## What It Refuses to Do

- **Single-node clusters never shed.** With no peer in the placement set there
  is nowhere safe to move bytes, so the rule is a no-op.
- **Below the high-water mark, nothing happens.** An idle loop is silent.
- **A failed push is not a move.** The local copy stays, the failure is audit
  logged, and the next cycle may succeed.
- **The last copy is never removed.** A blob is only dropped after one of its
  peers confirmed holding the bytes; a node alone in the placement keeps
  everything.

## The Other Trigger: A Peer Appears

Disk pressure is not the only reason bytes should move. When a node joins, the
placement function starts assigning new blobs to it — but the blobs it now owns
under the new placement may already be sitting elsewhere, written before it
arrived.

The same rule covers this case with one extra condition. On join, a node looks
at its local blobs and asks, for each: does placement now include the newcomer,
and does it still include me? If the answer is yes-then-no, the blob belongs on
the newcomer, so it is pushed and dropped locally. If placement still includes
this node, the blob stays: moving it would not change anyone's ownership, and
dropping it would reduce the replica count.

That single condition — "only migrate when I no longer own a copy" — is what
keeps membership rebalance from trading availability for tidiness. A node never
sheds itself out of the replica set of a blob it is supposed to hold.

In this system membership is declared in configuration, so "join" means a
configured peer becoming reachable. The rule is the same either way: the trigger
changes, the safety condition does not.

## Test What You Cannot See

A rebalance push is four steps the unit tests can each fake: dial the peer,
shake hands, negotiate metadata, stream the bytes, wait for the acknowledgement.
Fake all four and the code looks perfect while the real path is broken. Ours was.

The peer handshake carries a timestamp that the receiver checks for freshness —
a cheap defence against replaying a captured handshake. The rebalance code sent
zero. Every mocked test passed, because a mock does not check timestamps. Over a
real socket the peer rejected the connection and the sender saw an EOF.

The fix is one line — send the current time — but finding it needed a test that
goes through an actual TCP listener with the actual handshake. That test now
covers the whole sequence end to end, and it fails if the timestamp is stale, so
the regression cannot come back silently.

Two sequences are covered that way: a pressure-driven shed, and a join-driven
migration, where the blob is only dropped after the joining peer confirms it has
the bytes. Both use ephemeral ports and a real store on the receiving side, so
the test also proves the peer actually persists the object rather than merely
acknowledging it.

The lesson generalizes: protocols have rules that only the real implementation
enforces. When a code path exists only in production — a peer-to-peer push, a
replication fetch — a unit test with a fake transport proves the plumbing, not
the contract.

## What Is Still Missing

This is load balancing by disk pressure, not a full rebalance. When a node
joins, the existing data does not automatically flow toward it — only nodes that
are actually full shed. Replication repair after a node leaves is handled by the
self-heal loop, and failure-domain spread by placement, so this rule does not
duplicate either. Load-aware request redirection would need per-peer request
counters that do not exist yet, and is deliberately left out rather than faked.

## References / Dig deeper

- OpenSpec change: `openspec/changes/r11-stigmergic-rebalance`
- ADR: `docs/adr/0072-r11-stigmergic-rebalance.md`
- Tracking issue: https://github.com/alsotoes/momo/issues/1143
