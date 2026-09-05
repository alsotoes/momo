---
title: "S3 501 Discipline: Object-Level Subresources (tagging, acl, versionId, retention, legal-hold)"
date: 2026-08-24T21:13:07Z
draft: false
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
Object-level query parameters (`?tagging`, `?acl`, `?versionId`, `?retention`, `?legal-hold`) on `GET/PUT/DELETE /bucket/key` were silently ignored — falling into `GetObject`/`PutObject`/`DeleteObject`.

## The Problem

Same misrouting bug as bucket config, but at object level. Object-level subresources:
- `?tagging` — object tagging
- `?acl` — object ACL
- `?versionId` — specific version
- `?retention` — object retention
- `?legal-hold` — legal hold

All fell into `GetObject`/`PutObject`/`DeleteObject` handlers, returning wrong data or silently ignoring the operation.

## The Solution: Object-Level 501 Intercept

Added `unsupportedObjectSubresources` map (5 entries) + detector at `HandshakeServer` after `extractS3BucketAndKey` (key!="" guard):

| Subresource | Description |
|-------------|-------------|
| `tagging` | Object tagging |
| `acl` | Object ACL |
| `versionId` | Specific version |
| `retention` | Object retention |
| `legal-hold` | Legal hold |

**Implementation**: Sibling intercept branch after `key==""` block, guarded `key!=""` → `501 NotImplemented` (`syscall.ENOTSUP`).

## Guarded: Supported Operations Untouched

| Subresource | Reason |
|-------------|--------|
| `uploadId`/`partNumber` | Multipart upload |
| `uploads` | List multipart uploads |
| `delete` | DELETE support |

Also fixed orphaned `extractS3BucketAndKey` doc comment left by P3.

## Verification

- 10 test cases → 501 NotImplemented
- `uploadId`/`partNumber` → multipart routing intact
- `?tagging` on GET → 501 (previously 200 with object data)
- `?versionId` on GET → 501 (previously served current version)
- Bucket-root `?versioning` → still 501 (P3)

## Standards

Per [docs/STANDARDS.md](../STANDARDS.md): 🛡 **Sentinel** (honest error semantics, no silent misrouting).

## Follow-ups

- Remaining ops (Phase 5) → `s3-p5-remaining-subresource-501`
- Documentation: `ARCHITECTURE.md`, `COMPATIBILITY.md`, `PROTOCOL.md`

## Artifacts

- Spec: `openspec/changes/s3-p4-object-subresource-501/`
- Issue: #914
- PR: #... (merged)
