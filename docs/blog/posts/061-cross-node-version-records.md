---
title: "Version Records That Follow the Bytes"
date: "2026-11-06T12:00:00Z"
draft: false
post_type: architecture
tags:
  - r10
  - s3
  - versioning
  - replication
categories:
  - s3
summary: "Bucket versioning is easy to implement on one node and easy to get wrong on many. A version list, a version-addressed delete, or an object-lock check answered by a node that did not serve the write sees a different history. This change makes version records follow the object's replicas: a local write fans the record out over the peer transport, idempotent apply keeps the chain consistent, and failures are logged rather than turned into write errors."
artifacts:
  - type: spec
    path: openspec/changes/r10-cross-node-version-records
  - type: adr
    path: docs/adr/0087-r10-cross-node-version-records.md
related: ["060-r10-object-lock", "056-s3-versioning-on-cas", "062-stigmergic-disk-shedding"]
difficulty: "intermediate"
pattern: "replicated-metadata"
teaches:
  - "Why version metadata must be co-located with the data replicas"
  - "How to make a replicated metadata apply idempotent and monotonic"
  - "Why best-effort replication is the right failure mode for derived metadata"
---

## The Real Problem

Bucket versioning looks local until the second node exists. Node A serves a
write to a versioned bucket, appends a version record to its own database, and
returns a version id. Node B, holding the same object's replica, has never
heard of that version. Ask B to list versions and the object is missing. Ask B
to delete that specific version and it answers `NoSuchVersion`. Ask B whether a
legal hold applies and it answers with an empty record.

None of these are crashes. They are worse: a cluster that gives different
answers to the same question depending on which node took it.

## Where Version State Actually Belongs

The object's bytes are already placed by CRUSH: each object lands on a
deterministic replica set. The version chain is metadata *about those bytes*,
so the natural placement rule is the same one: put the version record on the
nodes that hold the copies.

That decision removes a whole class of design work. There is no second
placement algorithm to keep in sync with the data path, no shard ring that has
to be recomputed when membership changes, and no gap where a node holds the
blob but not the history. Delete markers have no hash to key on, so they use
the object name — the marker is still about the same key.

The first sketch for this feature assumed a metadata RPC layer that would
fan out by shard. Reading the code showed that layer was still a sketch —
methods returning canned replies, no server wiring. Building on it would have
meant finishing a second control plane first. The peer transport that already
carries gossip was right there, so the record rides that instead.

## Applying a Record You Did Not Create

Receiving a record is not the same as creating one. Three properties matter.

**Identity is the version id.** The record arrives with the id the writer
chose. Re-applying the same record must be a no-op, not a second version.
Compare the stored record with the incoming one and return early when they
match.

**Latest only moves forward.** Records carry a timestamp. A late-arriving
replica must not mark an older record as current just because the sender
believed it was. Compare timestamps and, when the incoming record is older,
store it *without* the latest flag. The chain stays complete; the head stays
correct.

**Replication must not recurse.** The store fires a hook after creating a
version locally. If the receive path used that same method, every applied
record would fan out again, and two nodes would talk to each other forever. The
hook fires only on the create path; the apply path has its own method and never
touches it.

## Failure Is Allowed to Be Boring

The write already succeeded before replication starts: the local record is
durable, the blob is durable, and the client has its version id. If a peer is
down, blocking the write would turn a metadata convenience into an availability
failure.

So delivery is best-effort. Each replica gets an acknowledged request with a
timeout; failures are written to the audit log with the peer id, and the
fan-out moves on. The record is not lost — the next write to that key
re-replicates the chain, and an operator reading the audit log sees exactly
which node missed which record.

This is a deliberate trade. Strong consistency for version metadata would need
quorum writes and conflict resolution, which is a much larger system than the
problem justifies. Version history is derived metadata: the bytes are the truth,
and the history can be repaired.

## What an Operator Sees Now

- A version created on any node appears in `GET ?versions` from the nodes that
  hold the object.
- Version-addressed reads, deletes, and lock checks return the same answer
  everywhere the object lives.
- The bucket's versioning status is carried along with the record, so a replica
  does not have to be told separately that the bucket is versioned.
- A node that missed a record logs it; it does not corrupt its chain.

## When NOT to Lean on This

This is replication of a chain, not consensus. Two nodes writing the same key at
the same instant can still produce records with near-equal timestamps; the
tie-break (version id) is deterministic, but the interleaving is whatever the
cluster happened to do. Workloads that need a single serialized history per key
need a different design.

It also does not make the plain `PUT` echo a version id. The gateway decides
its response before it has streamed the body, so that particular S3 nicety
remains documented and unfulfilled; multipart uploads do echo it.

## References / Dig deeper

- OpenSpec change: `openspec/changes/r10-cross-node-version-records`
- ADR: `docs/adr/0087-r10-cross-node-version-records.md`
- Tracking issue: https://github.com/alsotoes/momo/issues/1233
- Parent umbrella: https://github.com/alsotoes/momo/issues/938
