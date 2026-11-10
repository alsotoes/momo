---
title: "An Audit Log That Cannot Grow Forever"
date: "2026-11-09T10:00:00Z"
draft: false
post_type: architecture
tags:
  - audit
  - retention
  - storage
categories:
  - storage
summary: "A retention setting that is parsed but never applied is a promise the database cannot keep. Bounding a hash-chained audit log means deleting old rows without breaking the chain that proves the remaining ones are authentic."
artifacts: [{type: spec, path: openspec/changes/audit-log-retention}, {type: adr, path: docs/adr/0083-audit-log-retention.md}]
related: ["053-multitenancy-authorization-audit", "065-metadata-writes-that-need-a-majority"]
difficulty: "intermediate"
pattern: "retention-policy"
teaches:
  - "Why a hash-chained log cannot simply delete old rows"
  - "How a chain anchor keeps the retained window verifiable"
  - "Why retention is a deliberate loss of tamper evidence"
---

Every object store eventually grows an audit log. Ours records each read, write,
and delete from both the S3 gateway and the native protocols into a dedicated
bucket, chained so that an operator can prove afterwards that nobody edited the
history. The configuration exposes a retention window — how many days of history
to keep — and for a while that setting did exactly what a setting does when
nobody reads it: nothing.

The bucket grew. Every authenticated request appended a record, and no code path
ever removed one. On a cluster doing steady traffic the metadata database
inflates until disk pressure is the thing that finally reports the problem, which
is a poor way to discover that a documented policy was never implemented.

## Why Not Just Delete Old Rows

The obvious fix — drop everything older than the window — breaks the very
property the log exists for. Each entry stores the hash of the entry before it.
Verification walks from the oldest entry forward, recomputing hashes and checking
that each one points at its predecessor. Delete the first thousand entries and the
new first entry still claims a predecessor that no longer exists, so verification
reports a broken chain. The log would look tampered the moment it was pruned.

Two design options present themselves. You can recompute the entire chain after
each prune, which rewrites every surviving entry and destroys the evidence that
those entries were not modified. Or you can accept that the deleted prefix is
gone and record where the new chain begins.

We chose the second. Pruning stores the hash of the last deleted entry in a small
metadata record — a chain anchor. Verification seeds its expected predecessor from
that anchor instead of from empty string, so the first surviving entry matches,
and every entry after it matches its neighbour. Tampering anywhere inside the
retained window still fails verification, exactly as before.

## The Prune Itself

The deletion runs inside one write transaction over the log bucket: walk from the
oldest key, delete each entry whose timestamp is below the cutoff, remember the
last deleted entry's hash, and finish by writing the anchor. Doing it in one
transaction means a crash mid-prune cannot leave half the prefix removed with a
stale anchor. The walk stops at the first entry at or past the cutoff, so a
retention window that removes nothing costs one cursor seek.

The daemon drives it on a simple schedule. It prunes once at startup, so a node
that was offline for a month catches up the moment it returns rather than waiting
for the next scheduled pass, and then once every twenty-four hours. Retention is
measured in days, so a daily cadence keeps the log within one interval of the
policy without scanning it more often than necessary. The loop is cancelled with
the daemon and guarded against panics, like the other background workers.

Two counters make the mechanism observable: one counts the passes that ran, the
other the entries they removed. A prune that fails logs the error and still
advances the pass counter, so a persistently failing prune shows up as runs
without terminations instead of silence. A pass that removes nothing is quiet on
purpose — a log saying "removed zero entries" every day trains people to ignore
the log.

## The Trade-off, Stated Plainly

Retention is a deliberate loss of evidence. Once an entry ages past the window it
is gone, and no audit can prove what happened before the window began. That is the
point of a retention policy: the alternative is a log that grows until it is
unmanageable, which in practice means the operator disables auditing or truncates
the database by hand. A bounded, verifiable window is a better guarantee than an
unbounded one nobody maintains.

What the design preserves is the chain within the window. An attacker with
database access can still delete the whole log — that was always true — but they
cannot silently alter a retained entry and leave verification passing. The anchor
records that some history preceded the window, so its absence is visible rather
than assumed.

## What It Taught Us

The general lesson is larger than the audit log: a configuration key that is
parsed and validated but never read is worse than no key at all, because it
advertises a guarantee that does not exist. Validating `retention_days >= 1` made
the setting feel implemented. The check that actually matters is the one that
finds no caller.

The specific lesson is about integrity chains. Hash chaining makes history
immutable by construction, which quietly makes deletion a design problem rather
than a database operation. Any system that both chains its records and expires
them needs an explicit boundary marker; the anchor is that marker.

## References / Dig deeper

- OpenSpec change: `openspec/changes/audit-log-retention/`
- ADR: `docs/adr/0083-audit-log-retention.md`
- Tracking issue: [alsotoes/momo#1307](https://github.com/alsotoes/momo/issues/1307)
- Implementation: `src/storage/storage.go` (`PruneAuditLog`, `VerifyAuditLog`),
  `src/server/audit_retention.go` (prune loop)
