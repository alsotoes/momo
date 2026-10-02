---
title: 'Scatter-Gather and Lease Consensus: Quorum Math'
date: 2026-08-13 07:58:21+00:00
draft: false
post_type: architecture
tags:
- go
- p2p
- lease
- quorum
- governance
categories:
- p2p
summary: Scatter-gather lists across shard owners; leases with correct majority quorum
  — and the two nasty off-by-one/partition bugs found by audit.
artifacts:
- type: pr
  id: '806'
- type: pr
  id: '810'
- type: spec
  path: openspec/changes/gossip-scatter-lease
related:
- 016-p2p-gossip-swim
- 021-r3-write-durability-quorum
- 015-sentinel-security-audit
- 014-confidential-dedup-oprf
- 018-adaptive-scaling-peer-quality
- 020-r2-degraded-read-self-heal
- 032-r5-metrics-phases-2-4
---
Metadata in momo does not live in one database. Each node owns a slice — a
*shard* — of the key space, so no single machine has the whole picture. That
shapes two operations every S3-style cluster needs: listing objects, and
protecting mutable shared state. They are two halves of the same problem.

**Scatter-gather** is the listing half. To answer "what objects exist under this
prefix?", a node fans the question out to the shard owners, collects their partial
answers, and merges them into one sorted, deduplicated list — scatter the
request, gather the replies. **A lease** is the mutable-state half: a time-limited
grant that lets exactly one node modify something like a rename or a reference
count at a time. Granting a lease requires a **quorum**, a majority of the voting
peers to agree, so two network partitions cannot both hand out the same lease and
corrupt state — the failure mode known as **split-brain**.

## Scatter-gather listing

{{< diagram src="/diagrams/03-scatter-gather-lease.svg" alt="Scatter-gather and lease quorum" caption="Scatter-gather and lease quorum" >}}

A list request fans out to shard owners, merges their metadata lists, dedups by
content hash (so the same bytes stored under two names appears once), and
paginates the result. The filesystem layer later narrowed the fan-out to only the
shard owners that can actually hold the requested prefix, turning a cluster-wide
scatter into a handful of targeted requests.

## Leases and the majority-quorum bug class

A lease protects mutable shared state, and the correctness of the whole scheme
rests on one arithmetic rule: a lease is granted only when the number of votes is
a strict majority of the peer count. For three peers the majority is two; for
five it is three. Get that comparison wrong and the guarantee evaporates. Two
bugs found by audit show how.

- **The off-by-one majority.** The quorum test was written in a way that could
  resolve too low for odd peer counts, accepting fewer votes than a true
  majority. The fix is to require strictly more votes than half the peers,
  computed on the correct integer basis — for three peers, more than one means
  at least two.
- **The zero-quorum lease.** Worse, lease acquisition could succeed with *no*
  quorum at all while the cluster was partitioned — the split-brain enabler. The
  fix is to fail closed: no quorum, no lease, even if that means the operation is
  refused until the partition heals.

## Keeping the hot paths allocation-free

Both operations mutate shared state, so both follow a merge discipline that
avoids allocations on the request path: merge by content hash, in a stable order,
with no intermediate copies. The metadata-list encoder also gained length limits,
so a maliciously oversized entry cannot blow up the message buffer.

## Why this is a security posture, not just a correctness fix

A partition that grants a lease is how a split ring silently corrupts reference
counts, which is why the zero-quorum fix is treated as security-adjacent rather
than as a routine bug. It is the same fail-loud stance we take elsewhere: when
the system cannot prove it is safe to proceed, it refuses instead of guessing.

## References / Dig deeper

- Lease/quorum fixes: PR [#806](https://github.com/alsotoes/momo/pull/806),
  PR [#810](https://github.com/alsotoes/momo/pull/810).
- Spec: `openspec/changes/gossip-scatter-lease`.
- Filesystem listing design: `docs/momofs/IMPLEMENTATION.md`; implementation in
  `src/momofs`.
- Sibling posts: [016: Gossip, SWIM, and Membership](016-p2p-gossip-swim.md),
  [021: R3 Write Durability and Quorum](021-r3-write-durability-quorum.md),
  [015: Sentinel Security Audit](015-sentinel-security-audit.md),
  [018: Adaptive Scaling](018-adaptive-scaling-peer-quality.md).