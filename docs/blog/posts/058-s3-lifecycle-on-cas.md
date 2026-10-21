---
title: "S3 Lifecycle Rules on a Content-Addressed Store: Time as a First-Class Dimension"
date: "2026-10-21T03:35:13Z"
draft: false
post_type: architecture
tags:
  - s3
  - lifecycle
  - cas
  - sentinel
categories:
  - storage
  - s3
summary: "Lifecycle rules are the first feature that makes momo delete data on a schedule rather than on request — which turns age, storage class, and delete markers into correctness problems. This post walks through the expiration and transition engine, why a background worker is the only safe place to run it, and how expiration composes with versioning's delete markers."
artifacts:
  - type: spec
    path: openspec/changes/r10-s3-breadth
  - type: issue
    id: 938
related: ["056-s3-versioning-on-cas", "004-cas-content-addressable-store", "031-core-integrity-verification", "059-r10-notification"]
difficulty: "intermediate"
pattern: "background-reconciliation"
teaches:
  - "S3 lifecycle expiration and transition semantics"
  - "scheduling deletion safely on a CAS store"
  - "how expiration interacts with versioning delete markers"
---

Every storage system eventually grows a feature that deletes data without anyone asking. Until now, momo only removed an object when a client sent a DELETE: every byte on disk was there because a request put it there. Lifecycle rules change that. A bucket policy like "expire anything under `logs/` after 30 days" means the server now removes data on its own, on a clock — and that single change turns *time* into a correctness dimension.

This post covers momo's lifecycle implementation: expiration, transitions, and the reconciliation loop that drives them.

## The Real Problem: deletion without a request

Two S3 lifecycle actions matter in practice:

- **Expiration** — delete an object once it is older than N days.
- **Transition** — move an object to a colder storage class once it is older than N days.

Both depend on **age**, which no client tells the server. Age is `now − last-modified`. So the server must (a) remember when each object was written, (b) periodically compare that to the current time, and (c) act. That is a *reconciliation loop* — the same shape as garbage collection and scrubbing, but with a user-supplied policy.

The tempting shortcut is to compute expiration lazily: only when an object is read, check whether it should have expired. That is wrong for several reasons. An object nobody reads never expires, so a "delete after 30 days" rule silently keeps data forever. Billing, compliance, and data-minimization guarantees all break. And a read-path check would put a policy evaluation in the hot path of every GET — the opposite of what we want.

## Why "just run a cron" is not enough

The other naive approach is a one-shot sweep triggered by a timer that iterates and deletes. The problems are concurrency and ordering:

- A sweep must not race with in-flight writes. If an object is being re-uploaded as the sweep decides to expire it, you can delete the *new* version.
- On a **versioned** bucket, "expire" does not mean "delete the bytes". S3 semantics say expiration of the current version creates a **delete marker** — a tombstone that hides the object but preserves the previous version. Deleting the bytes would destroy history the user explicitly asked to keep.
- Transitions must not rewrite content. Moving an object to a colder class is a metadata change, not a data copy.

Each of these is a decision the storage layer — not the timer — has to make, because only the storage layer knows about versions, metadata, and locks.

## The Solution: a policy engine in storage, a clock in the server

The split follows the project's standing boundary: **storage owns correctness, the server owns scheduling.**

**The policy lives in storage.** A `LifecycleConfig` is an ordered list of rules, stored per bucket in its own BoltDB bucket. Each rule carries a prefix, an optional `ExpirationDays`, and an optional `TransitionDays` + `StorageClass`. The engine walks the bucket's objects, finds the first *enabled* rule whose prefix matches, and applies one action:

```go
switch {
case rule.ExpirationDays > 0 && ageDays >= rule.ExpirationDays:
    // expire: delete marker if versioned, permanent delete otherwise
case rule.TransitionDays > 0 && ageDays >= rule.TransitionDays && rule.StorageClass != "":
    // transition: update x-amz-storage-class in the object's S3 metadata
}
```

Objects whose modification time is unknown are skipped rather than guessed at — a wrong age would delete the wrong thing.

**The clock lives in the server.** A background worker ticks on `[s3] lifecycle_worker_interval` and calls `EvaluateLifecycle(bucket, now)`:

```go
case now := <-ticker.C:
    out, err := engine.EvaluateLifecycle(bucket, now)
    // log expired/transitioned counts
```

Keeping the clock in the server means the storage engine stays deterministic and trivially testable — the tests pass an arbitrary `now` and assert the outcome, with no sleeping and no timers. The worker itself is panic-safe (Rule 43) so a policy bug can never take down the daemon.

> **Principle:** separate *what should happen* (a pure policy evaluation, given a clock) from *when to check* (a scheduler). The deterministic half is where the correctness lives; the scheduler should be dumb enough to have no bugs.

## Expiration meets versioning

The most interesting interaction is with versioning, shipped in the previous phase. On an unversioned bucket, expiration is a plain delete. On a **versioned** bucket, the engine writes a delete marker first:

```go
func (s *CASStore) expireObject(bucket, name string, versioned bool) error {
    if versioned {
        if _, err := s.RecordDeleteMarker(bucket, name); err != nil {
            return err
        }
    }
    return s.Delete(name)
}
```

The delete marker becomes the new "latest" version, so a normal GET returns 404 — the object looks expired. But the previous version survives and is still reachable by `versionId`, which is exactly what S3 promises and what point-in-time recovery tools depend on. The two features compose without either knowing the other's internals: versioning provides `RecordDeleteMarker`, and lifecycle just calls it.

## Failure Modes

- **Lazy expiration.** Checking age on read means unread objects never expire. A background sweep is the only way "delete after 30 days" is actually true.
- **Expiring bytes on a versioned bucket.** Deleting content instead of writing a marker destroys version history. The versioned branch must be taken.
- **Racing an in-flight write.** The engine runs under the store's lock discipline, so a concurrent re-upload cannot be clobbered mid-expiry.
- **Unknown modification time.** Treating a missing timestamp as "very old" would delete fresh data. Unknown age is skipped.

## When NOT to Use This

A background sweep is the right shape when the policy is time-based and the object set is bounded enough to walk periodically. It is a poor fit for per-object TTLs at massive scale (a full scan becomes expensive — that wants an indexed time wheel or a priority queue), or for sub-second expiry precision. For momo's single-bucket gateway and day-granularity rules, a periodic reconciliation pass is the simplest thing that is correct.

## References / Dig deeper

- The standard: [docs/CORE/STANDARDS.md](../../STANDARDS.md)
- Related posts: [056](056-s3-versioning-on-cas.md) S3 versioning on a CAS
  store, [004](004-cas-content-addressable-store.md) content-addressed storage,
  [031](031-core-integrity-verification.md) core integrity verification.
- Tracking issue: [#938](https://github.com/alsotoes/momo/issues/938).
