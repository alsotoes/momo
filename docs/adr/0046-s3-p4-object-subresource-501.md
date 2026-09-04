# 0046-s3-p4-object-subresource-501

## Status
Proposed

## Confidence
High

## Context
Object-level subresource query params (`?tagging`, `?acl`, `?versionId`,
`?retention`, `?legal-hold`) on `GET/PUT/DELETE /bucket/key` are not implemented,
yet today they are silently ignored and misroute to the nearest method handler:
e.g. `GET /bucket/key?tagging` serves the object bytes as GetObject and
`PUT /bucket/key?acl` uploads as PutObject. Instead of silently misbehaving,
momo should answer a clean, S3-compliant `501 NotImplemented` — the same honest
refuse posture applied to bucket-config subresources (PR #913 / P3 #912) and
unsupported SSE (PR #906 / P1 #907).

## Decision
- clean rejection of unsupported object subresources: The system SHALL intercept a defined set of unsupported object subresource query params when addressed at an object key (`key != ""`) for any HTTP method, and SHALL respond `501` with the S3 `NotImplemented` error code without invoking store operations.
- supported object subresources unaffected: The system SHALL continue to route supported object params (`?uploadId`, `?partNumber`) exactly as before.
- bucket-root subresources unaffected: The system SHALL retain the bucket-config rejection introduced in #912/#913.

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
- Spec: openspec/changes/s3-p4-object-subresource-501/
- Blog: docs/blog/posts/...md
