# Change: S3 P4 — 501 for unsupported object-level subresources
**Related Issues:**
- https://github.com/alsotoes/momo/issues/914

## Why
Object-level subresource query params (`?tagging`, `?acl`, `?versionId`,
`?retention`, `?legal-hold`) on `GET/PUT/DELETE /bucket/key` are not implemented,
yet today they are silently ignored and misroute to the nearest method handler:
e.g. `GET /bucket/key?tagging` serves the object bytes as GetObject and
`PUT /bucket/key?acl` uploads as PutObject. Instead of silently misbehaving,
momo should answer a clean, S3-compliant `501 NotImplemented` — the same honest
refuse posture applied to bucket-config subresources (PR #913 / P3 #912) and
unsupported SSE (PR #906 / P1 #907).

## What Changes
- Add a small set of known-but-unsupported S3 **object** subresource query
  params and intercept them at the S3 dispatch root when an object key is
  addressed (`key != ""`), across all methods (GET/PUT/POST/DELETE).
- Interception returns S3 error `501` code `NotImplemented` (bounded write, no
  store dependency) before any object/list/multipart routing.
- Supported object params are untouched: `?uploadId` and `?partNumber`
  (multipart) continue to route as before.

## Non-Goals
- Any actual implementation of object ACLs, tagging, object-lock retention, or
  object versioning — reject-only honest posture.
- Bucket-root bucket-config subresources — already handled by #912/#913 (P3).
