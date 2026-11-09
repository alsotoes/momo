---
title: "Metadata Writes That Need a Majority"
date: "2026-11-08T06:57:14Z"
draft: false
post_type: architecture
tags:
  - p2p
  - metadata
  - quorum
categories:
  - storage
summary: "A distributed metadata catalog is only as good as its write path, and the write path was a stub that always said yes. This change makes an object's metadata reach a majority of its replica set before the write is acknowledged, and it uncovered an encoding bug that made every shard key unreadable."
artifacts: [{type: spec, path: openspec/changes/r6-metadata-ha-v2}, {type: adr, path: docs/adr/0073-r6-metadata-ha-v2.md}]
related: ["061-cross-node-version-records", "063-gossip-load-without-a-coordinator"]
difficulty: "advanced"
pattern: "quorum"
teaches:
  - "Why a write stub is more dangerous than a missing feature"
  - "How a majority of acks bounds data loss"
  - "Why encoding bugs hide behind green tests"
---

A function that returns success without doing anything is worse than a function
that returns an error. The error is visible in a test; the empty success is
visible only when someone needs the data and it is not there.

The distributed metadata catalog had exactly that shape. The RPC handlers existed,
the ring placed shards, the vector clocks were encoded — and the public write
methods were stubs that answered `{Success: true}`. Every earlier test passed,
because nothing had exercised the path end to end.

## What a Write Actually Needs

Metadata for one object lives on a replica set derived from its shard. A write is
acknowledged only when a strict majority of that set has the record: two of
three, three of five. That is the smallest number that makes two conflicting
minority writes impossible — any two majorities overlap on at least one replica,
so a later write can see and supersede the earlier one.

The mechanics are intentionally boring. The local replica, when this node is part
of the set, applies in-process. The rest receive a replication request carrying a
request ID, and the caller waits on a map keyed by that ID, not on "the next
response from anyone". A reply for an unknown request is dropped. Each call has a
bounded timeout, and a slow replica simply fails to contribute an ack instead of
blocking the write forever.

Partial success is an error. If a write reaches one of three replicas, the caller
is told it failed, because "the metadata is somewhere" is not a guarantee anyone
can build on.

## The Bug Behind the Stub

Filling in the write path immediately failed, on the second write, with
`truncated replica data`. The catalog had never stored a record successfully — not
because of the network, but because of four bytes of arithmetic.

The binary metadata format writes a length-prefixed shard key. The encoder
allocated a buffer sized `4 + len(shardKey)`, wrote the length into the first four
bytes, and appended **the whole buffer** — including `len(shardKey)` trailing zero
bytes — before appending the key itself. Every field after the key was then read
one key-length out of position: the replica count came from the wrong offset, and
decoding failed.

An empty shard key hid it perfectly, because zero trailing bytes shift nothing.
And an empty key is exactly what every test supplied. The moment the ring
derived a real shard key per object, every record became unreadable.

The fix is one line of intent: a four-byte header is four bytes, then the key. The
test that guards it now round-trips empty, ASCII, multibyte and combining-mark
keys, because a length prefix that is wrong for one encoding is wrong for all of
them.

## Why This Matters for Failover

Quorum writes are what make the rest of the story possible. A node that takes
over a read can compare vector clocks from multiple replicas and detect a
conflict instead of trusting whichever replica answered first. Read repair, hinted
handoff, and any-node reads all assume that a metadata write either happened
everywhere that matters or was reported as a failure — not that it quietly
half-happened.

## What Happens to the Replica That Was Down

A quorum write can succeed while one replica is unreachable. That replica then
holds nothing — not a stale record, no record at all. Read repair fixes it the
first time somebody reads that name, and the next write of the same name fixes it
too, but a delete could stay mistaken on that replica until either happens.

Hinted handoff closes the gap. The coordinator keeps the record it could not
deliver, keyed by the peer it owes it to, and waits. When the cluster's failure
detector reports that peer healthy again, the coordinator replays what it owes and
deletes each hint only after an acknowledgement. Replaying is safe because the
record carries its vector clock: a replica that already has something newer
ignores it. Hints expire after an hour and are capped per peer, so a node that has
been down for a week does not accumulate an unbounded queue — past that point the
record has to be recovered from a live replica anyway.

The important property is that storing a hint never fails the write. The quorum
already acknowledged it; refusing to store the note would trade a convergence
delay for a false failure.

## References / Dig deeper

- OpenSpec change: `openspec/changes/r6-metadata-ha-v2/`
- Tracking issue: https://github.com/alsotoes/momo/issues/1142
- Related post: cross-node version records (061)
