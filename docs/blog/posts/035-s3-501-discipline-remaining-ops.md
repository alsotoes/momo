---
title: "S3 501 Discipline: Remaining Ops — SelectObjectContent, UploadPartCopy, Analytics, Inventory, Metrics, Intelligent-Tiering"
date: 2026-08-24T21:44:12Z
draft: false
post_type: issue
tags: [s3, compatibility, sentinel]
categories: [s3]
summary: "Final 501 sweep: SelectObjectContent, UploadPartCopy, analytics, inventory, metrics, intelligent-tiering now return honest 501 NotImplemented."
artifacts:
  - {type: spec, path: openspec/changes/s3-p5-remaining-subresource-501}
  - {type: issue, id: "920"}
related:
  - 033-s3-501-discipline-bucket-config
  - 034-s3-501-discipline-object-subresources
  - 008-s3-gateway-core
---
Two earlier sweeps taught momo's S3 gateway to say "not implemented" for
unsupported bucket and object subresources. This is the last sweep: the handful of
operations that were not subresources at all but still slipped through to the
wrong handler. With these six fixed, every unsupported S3 operation we know of now
fails honestly.

## The Real Problem

Three kinds of operation were still misrouting:

- **`SelectObjectContent`** — a SQL-like query that filters an object's contents
  and streams the matching rows back. Unsupported, it fell through to a plain
  `GetObject` and returned the whole object.
- **`UploadPartCopy`** — a server-side copy that uses an existing object as one
  part of a multipart upload. Unsupported, it fell through to `UploadPart` and
  treated the copy request as an ordinary data upload.
- **Four bucket configuration subresources** — `analytics`, `inventory`,
  `metrics`, and `intelligent-tiering` — that fell through to object listing.

Each returned either the wrong data or a `200` over an operation that never
happened.

## Why the Obvious Fixes Failed

There was no single mechanism to extend. `SelectObjectContent` is a query action
on an object; `UploadPartCopy` is signalled by a request header, not a query
parameter; the four configuration subresources are bucket-level. A fix had to
cover all three shapes rather than lean on one router hook.

The tempting shortcut was to reject anything that looked unusual. That would have
broken supported features that share the same URLs and headers — multipart uploads
in particular.

## The Solution: Complete 501 Coverage

We extended both rejection lists and added one targeted intercept:

| Operation | How it is signalled | Where it is rejected |
|-----------|---------------------|----------------------|
| `SelectObjectContent` | Query on an object | Object-level rejection |
| `UploadPartCopy` | `X-Amz-Copy-Source` header on a multipart `PUT` | Dedicated check in the `PUT` dispatch, before the part-upload handler |
| `analytics` | Bucket configuration subresource | Bucket-level rejection |
| `inventory` | Bucket configuration subresource | Bucket-level rejection |
| `metrics` | Bucket configuration subresource | Bucket-level rejection |
| `intelligent-tiering` | Bucket configuration subresource | Bucket-level rejection |

The four configuration subresources joined the existing bucket-level list.
`SelectObjectContent` is a query *action*, not a subresource, so it needed its own
branch rather than a list entry. `UploadPartCopy` is distinguished by a header, so
it is checked in the `PUT` dispatch before the ordinary multipart part handler can
claim it — the one ordering-sensitive part of the change.

## Complete 501 Coverage

| Phase | Target | Count |
|-------|--------|-------|
| P1 | SSE-KMS / SSE-C | 2 |
| P3 | Bucket configuration subresources | 16 |
| P4 | Object-level subresources | 5 |
| P5 | Remaining operations | 6 |
| **Total** | **All known unsupported** | **29** |

## How We Verified

- All six operations return `501 Not Implemented`.
- The `UploadPartCopy` check runs before the part-upload handler, so there is no
  collision.
- Existing multipart operations (`uploadId`, `partNumber`) are untouched.
- Bucket configuration subresources such as `?versioning` still return `501` from
  the earlier phase.

## Failure Modes

| Risk | Guard |
|------|-------|
| `UploadPartCopy` intercepted after the wrong handler claims it | The check is placed before the part-upload branch in `PUT` dispatch |
| Over-rejecting normal multipart uploads | The copy check requires the copy-source header; a normal part upload does not carry it |
| A new unsupported operation slipping through | Both rejection lists and the dispatch intercept are the single place to add coverage |

## When NOT to Use This

- **For supported operations.** The lists contain only operations we do not
  implement. Never add a supported one.
- **As a substitute for implementation.** This is honest failure, not a feature.
  When we implement one of these, it leaves the list.

## Engineering Standards (🛡 Sentinel)

Per [docs/CORE/STANDARDS.md](../../STANDARDS.md): 🛡 **Sentinel** — complete, honest
`501` coverage with no silent misrouting.

## References / Dig deeper

- Spec: `openspec/changes/s3-p5-remaining-subresource-501/`.
- Issue: #920.
- Related posts: [008: S3 Gateway Core](008-s3-gateway-core.md),
  [033: Bucket Configuration Subresources](033-s3-501-discipline-bucket-config.md),
  [034: Object-Level Subresources](034-s3-501-discipline-object-subresources.md).
