---
title: "S3 Versioning on a Content-Addressed Store: Two Kinds of Identity"
date: "2026-10-11T08:56:13Z"
draft: false
post_type: architecture
tags:
  - s3
  - cas
  - versioning
  - bolt
categories:
  - storage
  - s3
summary: "Why S3 versioning and content-addressed storage seem to conflict — mutation identity vs content identity — and how momo keeps both: UUIDv4 version records pointing at deduplicated blobs, delete markers that shadow without deleting, and pagination that can only be computed after sorting. Teaches: the real semantics backup tools depend on, and where the version chain deliberately stops being distributed."
artifacts:
  - type: spec
    path: openspec/changes/r10-s3-breadth
related: ["055-adaptive-volume-storage", "031-core-integrity-verification", "026-metrics-observability", "058-s3-lifecycle-on-cas", "059-r10-notification", "060-r10-object-lock"]
difficulty: "intermediate"
pattern: "layered-identity"
teaches:
  - "S3 versioning semantics (delete markers, IsLatest, revival)"
  - "CAS dedup coexisting with per-PUT version ids"
  - "post-sort pagination over random version ids"
---

## The Real Problem

Backup tools — Veeam, restic, rclone — do not just upload files. They upload the same file many times and rely on S3 **versioning** to keep every upload addressable: "give me the object as it existed on Tuesday." Delete a file and the tool expects a *delete marker*, not a missing object — so it can restore Tuesday even though you deleted Wednesday.

momo's storage layer is **content-addressed (CAS)**: a blob's identity *is* its SHA-256 hash. Put the same file twice, get one physical blob. This is the property that makes deduplication free and integrity verification trivial.

At first glance the two models fight each other:

| | CAS (momo's core) | S3 versioning (what clients expect) |
|---|---|---|
| Identity of "the same file put twice" | One blob — same hash | **Two versions** — two version ids |
| Delete | Remove the mapping | **Create a delete marker** — nothing is removed |

If we derived version ids from content hashes, two uploads of identical content would collapse into one version — and point-in-time recovery would silently lose "Tuesday" whenever Tuesday's file was unchanged. That is exactly the bug a backup customer cannot see until the day they need the restore.

## The Design: Version Records Are Pointers, Not Copies

The resolution is to keep the two identity systems in separate layers and never let either borrow from the other:

- **Content identity stays CAS.** The blob layer does not change at all. A version record only *points at* a hash.
- **Mutation identity is a new metadata layer.** Every PUT to a versioned bucket appends a `VersionRecord`: an opaque **UUIDv4** id, the content hash, the size, a timestamp, and an `IsLatest` flag.

Because records only point at hashes, the space cost of uploading the same file a hundred times is **zero** — one physical blob, one hundred version records of a few dozen bytes. CAS dedup is not compromised; it is *composed with* versioning.

```go
type VersionRecord struct {
    VersionID      string // opaque UUIDv4 — never derived from content
    Hash           string // CAS blob reference ("" for delete markers)
    Size           int64
    Timestamp      int64  // unix nano — the ordering key
    IsLatest       bool   // exactly one record per key
    IsDeleteMarker bool   // shadows the object without deleting anything
}
```

Why UUIDv4 and not the hash? Because the version id must distinguish **events** (two uploads) while the hash distinguishes **bytes**. Conflating them is the design mistake this whole layer exists to avoid.

## Delete Markers: Shadowing, Not Deleting

In S3, `DELETE` on a versioned bucket does not delete anything. It appends a record whose only job is to say "newer than everything below me, and this key is gone." Plain reads see the marker and return 404 — but the underlying data versions are untouched.

The revival rule is the subtle part: delete **the marker itself** (by version id) and the object *comes back* — the previous data version becomes latest again. momo implements this transactionally in one bbolt update: remove the record, re-point `IsLatest` to the next-newest record, and re-point the flat namespace mapping so even non-version-aware reads follow the revival.

```go
if deleted.IsLatest {
    // Next-newest wins — even if it is another marker.
    next, ok, _ := latestVersionTx(tx, bucket, key)
    if ok && !next.IsLatest {
        next.IsLatest = true
        b.Put(versionKey(bucket, key, next.VersionID), marshal(next))
    }
}
```

A versioned delete of the *latest data version* works the same way: the marker that shadows it (if any) becomes latest again. Every path funnels through one primitive — "re-point IsLatest" — instead of each caller implementing revival differently.

## Pagination You Can Only Compute After Sorting

S3 lists versions in key order ascending, and within a key, **newest first**. momo's version ids are random UUIDs, which means the on-disk byte order of version records carries no relationship to that listing order. The first implementation tried to skip ahead with a cursor seek to the marker key — and lost records, because a seek lands in *UUID byte order*, which is the wrong order for everything the marker key still holds.

The fix is to stop seeking and just sort:

1. Scan all version records of the bucket (they are tiny metadata rows).
2. Sort: key ascending, then timestamp descending, then version id descending as the tie-break.
3. Find the `(keyMarker, versionIdMarker)` pair *in the sorted order* and resume after it.

```go
sort.SliceStable(entries, func(i, j int) bool {
    if entries[i].key != entries[j].key {
        return entries[i].key < entries[j].key
    }
    if entries[i].rec.Timestamp != entries[j].rec.Timestamp {
        return entries[i].rec.Timestamp > entries[j].rec.Timestamp // newest first
    }
    return entries[i].rec.VersionID > entries[j].rec.VersionID // stable tie-break
})
```

The lesson generalizes: **pagination markers must be computed in the same ordering the page will be served in.** A marker saved from one ordering and applied in another silently drops or duplicates rows — the kind of bug that only shows up when a customer pages through more than one screen of history.

## Where It Deliberately Stops (The Ceiling)

The version chain is **node-local** in this phase. Every S3 write records its version on the node that ingested it; replication of the version chain across nodes through the R6 metadata RPC is the tracked follow-up. The ceiling is real: if you lose the ingest node, you lose the version *history* (the blobs themselves are already replicated).

Likewise, plain PUT responses do not echo `x-amz-version-id` yet — the gateway's write response is composed before the body streams, so the header is not reachable on that path. Multipart completions (which are fully gateway-handled) do echo it, and any client can discover version ids via `GET ?versions`.

Both ceilings are written into the spec's task list, not hidden — a shortcut without a recorded upgrade path is just a future outage. The defensive patterns used throughout — bounded recovery on every public method, length-limited error bodies — are the repo's standing rules from `docs/CORE/STANDARDS.md`.

## When NOT to Use This

If your storage layer is not content-addressed, do not build versioning this way — a plain version table keyed by object name is simpler and has no dedup interplay to reason about. The layered-identity design earns its complexity only when the content layer already owns an identity system that versioning must not disturb. And if your clients never use delete markers or point-in-time reads, S3-style versioning is ceremony: object locking and lifecycle rules build on it, but a store without those requirements can stay single-version.

## References / Dig deeper

- Spec: `openspec/changes/r10-s3-breadth/` (R10-V1 — versioning requirements and scenarios)
- Tracking issue: alsotoes/momo#938 (R10 breadth; phases 2–4 = lifecycle, notification, object lock)
- Storage implementation: `src/storage/versioning.go` — version records, markers, pagination
- S3 surface: `src/transport/s3_versioning.go` — versioning config, ListVersionsResult XML, versioned deletes
- The CAS core the records point into: post 055 (adaptive volume storage), post 031 (core integrity verification)
- S3 versioning semantics: the AWS S3 API `VersioningConfiguration`, `ListVersionsResult`, and delete-marker behavior this implementation mirrors
