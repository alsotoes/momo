---
title: 'R2: Degraded-Read Survivor Fallback and Self-Heal'
date: 2026-08-27 03:25:15+00:00
draft: false
tags:
- go
- self-heal
- degraded-read
- repair
- durability
categories:
- durability
summary: When a replica is corrupt or unreachable, reads fall back to a survivor and
  the ring re-replicates — degraded-read and self-heal rebuild shipped.
artifacts:
- type: pr
  id: '953'
- type: spec
  path: openspec/changes/r2-self-heal-rebuild
- type: issue
  id: '928'
related:
- 019-r1-failure-domain-placement
- 007-at-rest-integrity-and-gc
- 021-r3-write-durability-quorum
- 017-scatter-gather-lease-quorum
- 018-adaptive-scaling-peer-quality
---
 R2: Degraded-Read Survivor Fallback and Self-Heal

R1 arranges replicas; R2 decides what the system does when a replica dies —
read from **survivors** and rebuild the lost copy. This is the degraded-read +
self-heal/rebuild arc (#953).

## Two mechanisms, one goal

1. **Degraded-read survivor fallback**: if the preferred replica is corrupt
   (verify-on-read mismatch, see [007](007-at-rest-integrity-and-gc.md)) or
   unreachable, reads fall through to a healthy survivor — the client gets
   data, **and** the corruption event is surfaced for repair.
2. **Self-heal rebuild**: the ring re-replicates to restore full replica
   count, using quality-ranked peer selection
   ([018](018-adaptive-scaling-peer-quality.md)) for the rebuild source.

## The trust invariant

These are **adaptive, mutating behaviors** — exactly the seam class in **Rule 74**:
compile-time interface seam + declarative policy; unknown/absent strategy names
fail closed; the read-verify chokepoint is never bypassed. Selection logic keeps
CRUSH + survivor ranking in the auditable core.

## Why this is durable (Sentinel lens)

Durability isn't "we have 3 copies" — it's *"any one copy dying doesn't lose the
object"*. Degraded-read turns a lost node from a data-loss event into a reads-
continue event; self-heal turns it into a *temporary* event. The P0 stack:
R1 placement ([019](019-r1-failure-domain-placement.md)) → R2 this → R3
durability barrier ([021](021-r3-write-durability-quorum.md)).

## Related

Placement: [019](019-r1-failure-domain-placement.md). Integrity input:
[007](007-at-rest-integrity-and-gc.md). Recovery bottom line:
[021](021-r3-write-durability-quorum.md).