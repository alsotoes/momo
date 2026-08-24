# Change: S3 P5 — 501 for remaining unsupported S3 operations
**Related Issues:**
- https://github.com/alsotoes/momo/issues/920

## Why
The honest `501 NotImplemented` posture from P3 (#912/#913), P4 (#914/#915) and
P1 (#906/#907) covers bucket-config and object-level subresources, but a few
unsupported S3 operations still silently misroute:
- `POST /bucket/key?select` (SelectObjectContent) is not a recognized POST
  subresource and falls through the dispatch.
- `PUT /bucket/key?partNumber=N&uploadId=X` with an `X-Amz-Copy-Source` header
  (UploadPartCopy) is misread by the UploadPart handler as a part-body upload.
- Bucket config subresources `?analytics`, `?inventory`, `?metrics`,
  `?intelligent-tiering` are not yet in the bucket-root reject set.

Momo should answer a clean, S3-compliant `501 NotImplemented` for each.

## What Changes
- Add `analytics`, `inventory`, `metrics`, `intelligent-tiering` to the
  bucket-root reject set (`unsupportedBucketConfigSubresources`).
- Add `select` to the object-level reject set (`unsupportedObjectSubresources`).
- Intercept UploadPartCopy in the PUT dispatch (before UploadPart): when
  `?uploadId` + `?partNumber` AND an `X-Amz-Copy-Source` header are present,
  return `501 NotImplemented`.
- All responses are bounded-write and store-independent, consistent with the
  existing rejection paths.

## Non-Goals
- Any actual implementation of SelectObjectContent, UploadPartCopy, or
  analytics/inventory/metrics/intelligent-tiering — reject-only honest posture.
- Changes to the already-covered P3/P4 reject sets.
