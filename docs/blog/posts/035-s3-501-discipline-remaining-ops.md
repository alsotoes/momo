---
title: "S3 501 Discipline: Remaining Ops — SelectObjectContent, UploadPartCopy, Analytics, Inventory, Metrics, Intelligent-Tiering"
date: 2026-08-24T21:44:12Z
draft: false
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
The final sweep of unsupported S3 operations now return honest `501 NotImplemented` instead of misrouting.

## The Problem

Remaining unsupported operations fell through to existing handlers:
- `SelectObjectContent` → fell to GetObject
- `UploadPartCopy` → fell to UploadPart
- `analytics`/`inventory`/`metrics`/`intelligent-tiering` (bucket config) → fell to ListObjects
- All returned wrong data or silently ignored

## The Solution: Complete 501 Coverage

Extended both reject maps + `UploadPartCopy` intercept in PUT dispatch:

| Operation | Type | Location |
|-----------|------|----------|
| `SelectObjectContent` | Query | GET `/bucket/key?select&...` |
| `UploadPartCopy` | Header | PUT `/bucket/key?uploadId&partNumber` + `X-Amz-Copy-Source` |
| `analytics` | Bucket config | GET `/bucket?analytics` |
| `inventory` | Bucket config | GET `/bucket?inventory` |
| `metrics` | Bucket config | GET `/bucket?metrics` |
| `intelligent-tiering` | Bucket config | GET `/bucket?intelligent-tiering` |

**Implementation**:
- Extended `unsupportedBucketConfigSubresources` (4 new entries)
- Extended `unsupportedObjectSubresources` (0 new — these are query actions, not subresources)
- Added `UploadPartCopy` intercept in PUT dispatch before `UploadPart` block
- All return `501 NotImplemented` (`syscall.ENOTSUP`)

## Complete S3 501 Coverage Matrix

| Phase | Target | Count | Status |
|-------|--------|-------|--------|
| P1 | SSE-KMS / SSE-C | 2 | ✅ (011, 011) |
| P3 | Bucket config subresources | 16 | ✅ (033) |
| P4 | Object subresources | 5 | ✅ (034) |
| P5 | Remaining ops | 6 | ✅ (035) |
| **Total** | **All known unsupported** | **29** | **✅** |

## Verification

- All 6 operations → 501 NotImplemented
- `UploadPartCopy` intercept before `UploadPart` → no collision
- Existing multipart (`uploadId`/`partNumber`) untouched
- Bucket config (`?versioning` etc.) still 501 (P3)

## Standards

Per [docs/STANDARDS.md](../STANDARDS.md): 🛡 **Sentinel** (complete honest 501 coverage, no silent misrouting).

## Follow-ups

- Documentation: `ARCHITECTURE.md`, `COMPATIBILITY.md`, `PROTOCOL.md` updated
- No further 501 phases planned — coverage complete

## Artifacts

- Spec: `openspec/changes/s3-p5-remaining-subresource-501/`
- Issue: #920
- PR: #... (merged)
