---
title: 'R3: Write Durability — fsync-Before-Ack, Survivor Quorum'
date: 2026-08-27 19:24:05+00:00
draft: false
post_type: architecture
tags:
- go
- durability
- fsync
- quorum
- consistency
categories:
- durability
summary: 'Ack only what survived: fsync/group-commit durability barrier, survivor
  quorum, and read-your-writes — the write-side of the P0 stack.'
artifacts:
- type: pr
  id: '954'
- type: spec
  path: openspec/changes/r3-durability-consistency
- type: spec
  path: openspec/changes/add-replication-durability-floor
- type: issue
  id: '928'
related:
- 019-r1-failure-domain-placement
- 020-r2-degraded-read-self-heal
- 017-scatter-gather-lease-quorum
- 002-replication-strategies-polymorphic
- 028-roadmap-and-research
---
R3: Write Durability — fsync-Before-Ack, Survivor Quorum

R1/R2 answer "what happens when data dies"; R3 answers the write-side question:
**what does success mean?** #954 shipped the durability barrier: `fsync`-
before-ack, a survivor quorum, and group-commit batching.

## The guarantee

- **fsync-before-ack**: a write is acknowledged only after the blob has *durably
  hit disk* (not just page cache) on the surviving replicas. No more "ACK then
  restart = lost object".
- **Group commit / none-fsync modes** (`openspec/changes/r3-durability-consistency/`):
  a configurable barrier — `fsync`, `group-commit`, or `none` — so operators
  trade strictness for throughput, never silently.
- **Survivor quorum**: success requires the *required* surviving replicas to
  confirm the fsync, aligned with lease/quorum math from
  [017](017-scatter-gather-lease-quorum.md).
- **Read-your-writes**: after an acknowledged write, a subsequent read returns
  the acknowledged object.

## The durability floor in practice

`add-replication-durability-floor` (#827) and the replication-forwarding fixes
bounded the whole layer: no silent `ACK` when forwarding failed, no blocking
semaphore without a context deadline, correctness on partial delete propagation
(all surfaced in the audit-era batch — [015](015-sentinel-security-audit.md)).

## Sentinel lens

fsync-before-ack is the Sentinel reading of "success": a client must *never*
observe a lie about durability. Combined:

- R1 [019](019-r1-failure-domain-placement.md) — where replicas live
- R2 [020](020-r2-degraded-read-self-heal.md) — what survives corruption
- R3 (this) — what "written" means

Together they discharge the P0 durability/consistency mandate (issue #928).