---
title: "S3 501 Discipline: Object-Level Subresources (tagging, acl, versionId, retention, legal-hold)"
date: 2026-08-24T21:13:07Z
draft: false
post_type: issue
tags: [s3, compatibility, sentinel]
categories: [s3]
summary: "5 object-level query parameters now return honest 501 NotImplemented instead of silently falling into GetObject/PutObject/DeleteObject."
artifacts:
  - {type: spec, path: openspec/changes/s3-p4-object-subresource-501}
  - {type: issue, id: "914"}
related:
  - 033-s3-501-discipline-bucket-config
  - 035-s3-501-discipline-remaining-ops
  - 008-s3-gateway-core
---
The previous post in this series gave bucket configuration operations an honest
"not implemented" answer. The same misrouting bug, however, lived one level
deeper: on the object itself. Five object-level query parameters were being
ignored, and a client that asked to read an object's tags got the object's bytes
instead. This post closes that gap.

## The Real Problem

In S3, an operation can be selected by a query parameter appended to an object's
URL rather than by the URL path. These are **subresources** — `GET /bucket/key`
reads an object, while `GET /bucket/key?tagging` reads that object's tags.

Five object-level subresources were unimplemented:

- `?tagging` — the object's tag set
- `?acl` — the object's access control list
- `?versionId` — a specific historical version
- `?retention` — object retention settings
- `?legal-hold` — a legal hold

None of them had a handler. The router fell through to the ordinary object
handlers, so a `GET` with `?tagging` ran the normal "read the object" path and
returned the object body. The client's tag-parsing code then choked on binary
data, or worse, silently treated it as an empty tag set.

## Why the Obvious Fixes Failed

The trade-offs here are the same as for the bucket-level case, and so is the
tempting wrong answer: return `404`. But the object exists; it is the *operation*
that is unsupported. A `404` would tell a client the object is missing and send it
down the wrong retry path.

Implementing the five operations was also out of scope: retention and legal hold
carry compliance semantics we did not want to half-build.

## The Solution: Object-Level 501 Intercept

We added a sibling rejection branch to the one used for bucket configuration. It
runs after the bucket and key have been parsed and only fires when an object key
is present. A request that names one of the five subresources gets **501 Not
Implemented**, the same honest answer as at the bucket level.

| Subresource | Meaning |
|-------------|---------|
| `tagging` | Object tag set |
| `acl` | Object access control list |
| `versionId` | A specific object version |
| `retention` | Object retention |
| `legal-hold` | Legal hold |

The split between the two rejection lists is deliberate. Bucket-level
subresources are checked when the key is empty; object-level ones when the key is
present. That is what lets `?tagging` mean "bucket tags" at the root and "object
tags" on a key without the two checks colliding.

## Guarding What We *Do* Support

The intercept must not swallow the multipart operations that legitimately use
query parameters on an object URL:

| Subresource | Reason it must pass through |
|-------------|------------------------------|
| `uploadId` / `partNumber` | Multipart upload parts |
| `uploads` | List multipart uploads |
| `delete` | Batch delete |

## How We Verified

- Ten request cases across the five subresources all return `501 Not Implemented`.
- Multipart routing on `uploadId` and `partNumber` is intact.
- `?tagging` on a `GET` now returns `501`, where it previously returned `200` with
  the object body.
- `?versionId` on a `GET` now returns `501`, where it previously served the
  current version.
- A bucket-root `?versioning` still returns `501` from the earlier fix.

## Failure Modes

| Risk | Guard |
|------|-------|
| Catching multipart requests | The intercept checks the subresource against a fixed list; multipart parameters are not in it |
| Colliding with the bucket-level check | The two branches are guarded by key-presence (empty key vs present key) |
| Stale documentation on the shared parser | The orphaned doc comment on the bucket/key parser left by the previous change was corrected here |

## When NOT to Use This

- **When the operation is supported.** Never add a supported subresource to the
  rejection list just because it is uncommon.
- **When the request is a normal object read.** The intercept only triggers on the
  named subresources; an unadorned `GET` still reads the object.

## Engineering Standards (🛡 Sentinel)

Per [docs/STANDARDS.md](../../STANDARDS.md): 🛡 **Sentinel** — honest error
semantics, no silent misrouting. An unsupported operation says so.

## References / Dig deeper

- Spec: `openspec/changes/s3-p4-object-subresource-501/`.
- Issue: #914.
- Related posts: [008: S3 Gateway Core](008-s3-gateway-core.md),
  [033: Bucket Configuration Subresources](033-s3-501-discipline-bucket-config.md),
  [035: Remaining Operations](035-s3-501-discipline-remaining-ops.md).
