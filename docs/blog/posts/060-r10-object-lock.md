---
title: "R10 Phase 4: S3 Object Lock — WORM That Actually Refuses Deletes"
date: "2026-10-28T12:00:00Z"
draft: false
post_type: architecture
tags:
  - r10
  - s3
  - object-lock
  - worm
  - compliance
categories:
  - s3
summary: "Object Lock is the S3 feature regulated workloads ask for first: write-once-read-many retention that the storage server itself enforces, not a policy the client promises to honor. This phase gives momo bucket-level lock enablement at creation, per-version retention and legal holds, and a delete path that answers 403 instead of quietly removing bytes. Teaches: how COMPLIANCE and GOVERNANCE differ in who can override them, why lock state must live beside the version record rather than inside object metadata, and which parts of AWS's surface momo deliberately does not pretend to implement."
artifacts:
  - type: spec
    path: openspec/changes/r10-s3-breadth
  - type: adr
    path: docs/adr/0058-r10-s3-breadth.md
related: ["059-r10-notification", "058-s3-lifecycle-on-cas", "056-s3-versioning-on-cas", "034-s3-501-discipline-object-subresources"]
difficulty: "intermediate"
pattern: "write-once-storage"
teaches:
  - "S3 Object Lock semantics: COMPLIANCE, GOVERNANCE, legal holds"
  - "Why retention must be keyed by version, not by object name"
  - "How to refuse destructive operations at the storage layer, not just in the HTTP handler"
---

## The Real Problem

Every storage system eventually meets a workload that cannot be allowed to
delete its own data. Financial records must survive a compliance window.
Backups must survive a compromised application account. Legal teams issue holds
that outlive any bucket policy.

The naive answer is "the client promises not to delete." That promise is worth
exactly nothing the moment the same credentials are used by a bug, a script, or
an attacker. The point of S3 Object Lock is that the server enforces the
promise: a locked version cannot be removed until its retention expires, no
matter who asks.

Two independent mechanisms exist in the S3 model:

- **Retention** — a deadline attached to a version, in one of two modes.
  `GOVERNANCE` mode may be bypassed by a caller holding a special permission
  (`x-amz-bypass-governance-retention: true`). `COMPLIANCE` mode may not be
  bypassed by anyone, including the bucket owner, until the deadline passes.
- **Legal hold** — a boolean flag with no expiry. It is set and cleared by an
  authorized caller, independent of retention, and it overrides everything.

## Why Lock State Belongs to the Version

An object name is not a stable identity. Overwrite `report.csv` five times and
you have five different byte streams behind one key. If lock state were stored
as object metadata, the sixth write would silently discard the previous
version's retention — and with it the entire guarantee.

So the lock record is keyed by the triple **bucket, key, version**. It lives in
its own storage bucket beside the version records that R10's versioning phase
introduced. The consequences are worth stating plainly:

- Overwriting a locked key is **allowed**. It creates a new version. The locked
  version is untouched, so WORM is preserved without blocking ordinary writes.
- Deleting the *name* is allowed. That creates a delete marker, which hides the
  object from plain reads while every locked version remains recoverable.
- Deleting a *specific version* is where enforcement happens: if that version
  carries an active retention deadline or a legal hold, the delete is refused.

That third rule is the one that has to be implemented inside the storage layer,
not only in the HTTP handler. A handler-level check protects one protocol; the
native protocol, a maintenance job, or a future API would walk straight past
it. Momo's version store therefore returns a permission error for a protected
delete, and each protocol maps that error into its own vocabulary — HTTP 403
`AccessDenied` for the S3 surface.

## Enabling Lock: Creation Only

AWS lets you turn Object Lock on only when the bucket is created. Momo keeps
that rule for the same reason AWS does: a lock that can be switched off after
the fact protects nobody. The gateway honors the creation-time header
`x-amz-bucket-object-lock-enabled: true`, and enables versioning implicitly,
because retention without versions is meaningless.

A later `PUT ?object-lock` is refused with `409 InvalidBucketState`, whether it
tries to enable or disable. `GET ?object-lock` reports the enabled document, or
`404 NoSuchObjectLockConfiguration` for an ordinary bucket. A storage backend
without lock support answers an honest `501 NotImplemented` rather than
pretending — the same discipline momo applies to every unsupported corner of
the S3 surface.

Two smaller decisions follow from the same logic. Enabling lock on a bucket
whose versioning was suspended is refused, because WORM would silently stop
capturing versions. And `COMPLIANCE` retention may be extended but never
shortened or downgraded to `GOVERNANCE` while it is active — a rule enforced by
comparing the new state against the stored one, using the clock supplied by the
request rather than a storage-side timer.

## The Capture Ceiling, Stated Out Loud

A single-part `PUT` in momo's gateway streams the request body to storage while
the response is decided; the version identifier is only known after the write.
That ordering makes it impossible to attach lock headers to that version during
the first phase. Rather than fake success, the phase documents the ceiling:
lock headers are captured on multipart completion, where the version id exists
before the response is sent. Clients that need retention on single-part writes
use multipart uploads, which backup and archival tools already do.

This is the same honesty rule the gateway applies elsewhere: a feature either
works, or it says clearly that it does not.

## What an Operator Sees

Reading a specific version now echoes the lock state back:

- `x-amz-object-lock-mode` — `COMPLIANCE` or `GOVERNANCE`
- `x-amz-object-lock-retain-until-date` — the deadline, in S3 timestamp form
- `x-amz-object-lock-legal-hold-status` — `ON` when a hold is set

A delete attempt against a protected version returns `403 AccessDenied` and
leaves the record intact. A `GOVERNANCE`-locked version yields to a request
carrying the bypass header; a `COMPLIANCE`-locked or held version never does.

## Failure Modes Worth Knowing

- **Clock trust.** Retention compares against the request clock, which is the
  same assumption S3 makes. A node with a badly skewed clock will evaluate
  deadlines incorrectly; keep NTP healthy, as you would for TLS validity.
- **Versioning suspension.** Once lock is enabled, momo refuses to suspend
  versioning, because doing so would break the WORM contract silently.
- **The native protocol.** Lock state is enforced by the version store, so
  deletes that go through S3 are covered. The native protocol's plain delete
  operates on names and produces a tombstone without consulting lock records —
  an intentional boundary, documented rather than hidden.

## When NOT to Use This

Object Lock is not a backup strategy. It prevents deletion of versions that
exist; it does not create copies, survive disk failure, or replicate across
failure domains. Use it together with replication and scrubbing, not instead of
them. It is also a poor fit for high-churn working data: every retained version
keeps its bytes referenced, and storage grows monotonically until retention
expires.

## References / Dig deeper

- OpenSpec change: `openspec/changes/r10-s3-breadth`
- ADR: `docs/adr/0058-r10-s3-breadth.md`
- Tracking issue: https://github.com/alsotoes/momo/issues/938
- Related phases: S3 versioning, S3 lifecycle, S3 notifications
