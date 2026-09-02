# 0045-s3-p5-remaining-subresource-501

## Status
Proposed

## Confidence
Medium

## Context
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

## Decision
- bucket analytics/inventory/metrics/intelligent-tiering rejected: The system SHALL reject `?analytics`, `?inventory`, `?metrics` and `?intelligent-tiering` addressed at a bucket root (`key == ""`) with `501` code `NotImplemented`, consistent with the existing bucket-config reject set.
- SelectObjectContent rejected: The system SHALL reject `?select` addressed at an object key with `501` code `NotImplemented`.
- UploadPartCopy rejected: The system SHALL reject a `PUT` carrying both multipart params (`?uploadId`, `?partNumber`) and an `X-Amz-Copy-Source` header with `501` code `NotImplemented`, instead of misreading the copy source as a part body.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Partial
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/s3-p5-remaining-subresource-501/
- Blog: docs/blog/posts/...md
