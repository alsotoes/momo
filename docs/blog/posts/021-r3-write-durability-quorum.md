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

Two earlier milestones answered "what happens when data dies" — where copies
live, and how we recover a corrupted one. But they left the most uncomfortable
question untouched: when we tell a client a write succeeded, what exactly have we
promised?

For a long time the honest answer was "not much." We returned success as soon as
the bytes were handed to the operating system. If the machine lost power a
millisecond later, those bytes might still be sitting in volatile page cache and
simply vanish. The client believed its data was safe. It was not.

## The Real Problem

A write acknowledgement is a promise about the future: *this data will survive*.
If we acknowledge before the data has actually reached durable storage, every
restart window becomes a silent data-loss window. "ACK then restart = lost
object" is exactly the kind of guarantee failure that erodes trust — not loud,
not crashy, just quietly missing data.

Two notions of "written" were being conflated:

- the bytes are in memory somewhere in the pipeline, and
- the bytes are safely on disk on enough machines to survive a failure.

R3 makes the acknowledgement mean the second one.

## Why the Obvious Solutions Failed

**"Acknowledge immediately, persist in the background."** Fast, and wrong for
durability. It trades a latency win for invisible data loss on crash.

**"Always fsync on every write."** Correct, and painfully slow. Forcing a disk
flush on every small write serializes throughput and turns a fast engine into a
sluggish one.

**"Acknowledge after one node writes."** This ignores the whole point of
replication: if that one node dies, the "successful" write is gone. Success has
to reflect what *survived*, not what one machine happened to accept.

## The Solution: Define Success Explicitly

R3 ships a **durability barrier** with three movable parts, so operators can pick
the trade-off deliberately instead of inheriting whatever the implementation
happened to do.

{{< diagram src="/diagrams/10-durability-barrier.svg" alt="R3 Write durability barrier" caption="R3 Write durability barrier" >}}

- **fsync-before-ack.** A write is acknowledged only after the blob has durably
  hit disk — not just page cache — on the surviving replicas. `fsync` is the
  system call that forces the operating system to flush buffered writes all the
  way to physical media; before it returns, the data is genuinely persistent.
  This is the change that kills the "ACK then restart = lost object" window.
- **Group commit and a `none` mode.** A configurable barrier with three settings:
  `fsync` (flush every write, strictest), `group-commit` (batch several writes
  into one flush, trading a little latency for a lot of throughput), and `none`
  (no flush, fastest, least durable). Operators choose their strictness
  explicitly rather than discovering it by accident.
- **Survivor quorum.** Success requires the required number of surviving replicas
  to confirm the flush. A **quorum** is simply a threshold of confirmations — you
  only report success when enough independent copies have agreed the data is
  safe, following the same lease and quorum logic we use elsewhere for
  consistency.
- **Read-your-writes.** After an acknowledged write, a subsequent read of that
  object returns the acknowledged data — never an older version. This closes the
  confusing gap where a client writes something, reads it back, and finds stale
  bytes.

### Group commit, plainly

The interesting design choice is group commit. Instead of one flush per write, we
let a small batch of writes accumulate and share a single flush. Ten writes that
would have caused ten disk round trips now cause one. The writers all wait for
the same flush and are acknowledged together. Latency per write rises slightly;
throughput rises sharply. For workloads with many small writes, that is a good
trade — and crucially, it is an *explicit* one.

## The Durability Floor in Practice

The durability work also closed a set of silent correctness gaps around
replication forwarding. A later audit-era batch bounded the whole layer:

- no silent acknowledgement when forwarding to a replica failed;
- no blocking on a semaphore without a context deadline (so a stuck replica could
  not hang a writer forever); and
- correct behavior when a delete was only partially propagated.

Each of these was the same class of bug: the system reporting success for work it
had not actually completed. R3's principle — *ack only what survived* — is what
let us find and fix them.

## The Sentinel Lens

fsync-before-ack is the Sentinel reading of the word "success": a client must
never observe a lie about durability. The three R-milestones fit together:

- **R1** — [where replicas live](019-r1-failure-domain-placement.md).
- **R2** — [what survives corruption](020-r2-degraded-read-self-heal.md).
- **R3 (this)** — what "written" actually means.

Together they discharge the P0 durability and consistency mandate.

## What Could Go Wrong

- **`none` mode is genuinely unsafe.** It exists so the trade-off is explicit,
  not so it is free. A crash can silently lose acknowledged writes.
- **A quorum that cannot be reached.** If too few survivors are healthy, the
  write fails rather than lying. That is correct, but it means availability
  depends on having enough replicas in distinct failure domains.
- **Group commit under light load.** With only one write in flight there is
  nothing to batch, so latency looks like plain fsync. The win only appears under
  concurrency.

## When NOT to Use This

- **Scratch or regenerable data.** If losing a write after a crash is genuinely
  acceptable, `none` mode is a legitimate choice — just make it a conscious one.
- **When the bottleneck is the network, not the disk.** fsync-before-ack adds
  latency; if disks are not the durability risk you care about, measure before
  paying for strictness.
- **As a substitute for replication.** The barrier tells you what survived. It
  does not create copies — R1 and R2 do that.

## References / Dig deeper

- Failure-domain placement: [019](019-r1-failure-domain-placement.md).
- Degraded read and self-heal: [020](020-r2-degraded-read-self-heal.md).
- Scatter-gather lease quorum: [017](017-scatter-gather-lease-quorum.md).
- Replication strategies: [002](002-replication-strategies-polymorphic.md).
- Production readiness roadmap: [028](028-roadmap-and-research.md).
- Specs: `openspec/changes/r3-durability-consistency`,
  `openspec/changes/add-replication-durability-floor`; PR #954; tracking issue
  #928.
- Audit-era durability batch: [015](015-sentinel-security-audit.md).
