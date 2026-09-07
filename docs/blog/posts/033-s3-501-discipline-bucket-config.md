---
title: "S3 501 Discipline: Honest 'Not Implemented' for 16 Bucket Config Subresources"
date: 2026-08-24T20:00:50Z
draft: false
post_type: issue
tags: [s3, compatibility, sentinel]
categories: [s3]
summary: "16 bucket config subresources (versioning, ACL, policy, CORS, lifecycle, etc.) now return honest 501 NotImplemented instead of silently misrouting to ListObjects/GetObject."
artifacts:
  - {type: spec, path: openspec/changes/s3-p3-bucket-subresource-501}
  - {type: issue, id: "912"}
related:
  - 008-s3-gateway-core
  - 009-s3-multipart-and-breadth
  - 011-s3-https-tls-enforcement
  - 034-s3-501-discipline-object-subresources
  - 035-s3-501-discipline-remaining-ops
---
Previously, unsupported bucket-level query parameters (`?versioning`, `?policy`, `?cors`, etc.) silently fell through to `ListObjectsV2`/`GetObject` — a silent misrouting bug. Now: **honest 501 NotImplemented**.

## The Problem

S3 bucket config subresources are well-defined operations (versioning, ACL, policy, CORS, lifecycle, etc.). Momo's S3 gateway didn't implement them, but instead of returning `501 NotImplemented`, it silently routed them to `ListObjectsV2` or `GetObject` — returning wrong data, wrong status codes, or silently ignoring the request.

## The Solution: Explicit 501 Interception

Added `unsupportedBucketConfigSubresources` map (16 entries) + detector at `HandshakeServer` after `extractS3BucketAndKey`:

| Subresource | Description |
|-------------|-------------|
| `versioning` | Bucket versioning config |
| `versions` | List all versions |
| `acl` | Access control list |
| `policy` | Bucket policy |
| `cors` | Cross-origin resource sharing |
| `website` | Static website hosting |
| `lifecycle` | Lifecycle rules |
| `tagging` | Bucket tagging |
| `encryption` | Default encryption |
| `publicAccessBlock` | Public access block |
| `accelerate` | Transfer acceleration |
| `replication` | Cross-region replication |
| `requestPayment` | Request payer |
| `logging` | Access logging |
| `objectLock` | Object lock config |
| `notification` | Event notifications |

**Implementation**: Intercepted at `HandshakeServer` right after `extractS3BucketAndKey` (key=="" guard), all HTTP methods → `501 NotImplemented` (`syscall.ENOTSUP`), bounded write.

## Guarded: Supported Subresources Untouched

| Subresource | Status |
|-------------|--------|
| `location` | 200 OK (bucket region) |
| `list-type` | 200 OK (ListObjectsV2) |
| `uploads` | 200 OK (ListMultipartUploads) |
| `uploadId`/`partNumber` | Multipart upload ops |
| `delete` | DELETE support |

## Verification

- 9 unsupported subresources → 501 NotImplemented
- `location` → 200 OK
- `list-type=2` → 200 OK (ListObjectsV2)
- `object-tagging` (bucket-level) → 404 Not Found (correct: object-level only)
- Multipart routing (`uploads`, `uploadId`) untouched

## Standards

Per [docs/STANDARDS.md](../../STANDARDS.md): 🛡 **Sentinel** (honest error semantics, no silent misrouting, fail-closed).

## Follow-ups

- Object-level subresources (Phase 4) → `s3-p4-object-subresource-501`
- Remaining ops (Phase 5) → `s3-p5-remaining-subresource-501`
- Documentation: `ARCHITECTURE.md`, `COMPATIBILITY.md`, `PROTOCOL.md`

## Artifacts

- Spec: `openspec/changes/s3-p3-bucket-subresource-501/`
- Issue: #912
- PR: #... (merged)
