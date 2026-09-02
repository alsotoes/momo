# 0043-s3-p3-bucket-subresource-501

## Status
Proposed

## Confidence
High

## Context
S3 bucket-config subresource query params (`?versioning` / `?versions`,
bucket ACLs & policies, `?cors`, `?website`, `?lifecycle`, `?tagging`) are not
implemented, yet today they fall through to the nearest method handler: e.g.
`GET /bucket?versioning` misroutes into ListObjectsV2 and
`PUT /bucket?tagging` misroutes into CreateBucket. Instead of silently
misbehaving, momo should answer a clean, S3-compliant `501 NotImplemented` —
the same honest reject-and-document posture already applied to unsupported SSE
(PR #906 / P1 #907).

## Decision
- clean rejection of unsupported bucket-config subresources: The system SHALL intercept a defined set of unsupported bucket-config subresource query params when addressed at a bucket root (`key == ""`) for any HTTP method, and SHALL respond `501` with the S3 `NotImplemented` error code without invoking store operations.
- supported subresources unaffected: The system SHALL continue to route supported subresources exactly as before.
- object-level subresources remain out of scope: The system SHALL NOT change routing for object-targeted subresources (e.g. `GET /bucket/key?tagging`, `?versionId`), which belong to Tier P4.

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
- Spec: openspec/changes/s3-p3-bucket-subresource-501/
- Blog: docs/blog/posts/...md
