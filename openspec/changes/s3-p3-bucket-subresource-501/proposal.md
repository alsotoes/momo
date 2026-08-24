# Change: S3 P3 — 501 for unsupported bucket-config subresources
**Related Issues:**
- https://github.com/alsotoes/momo/issues/912

## Why
S3 bucket-config subresource query params (`?versioning` / `?versions`,
bucket ACLs & policies, `?cors`, `?website`, `?lifecycle`, `?tagging`) are not
implemented, yet today they fall through to the nearest method handler: e.g.
`GET /bucket?versioning` misroutes into ListObjectsV2 and
`PUT /bucket?tagging` misroutes into CreateBucket. Instead of silently
misbehaving, momo should answer a clean, S3-compliant `501 NotImplemented` —
the same honest reject-and-document posture already applied to unsupported SSE
(PR #906 / P1 #907).

## What Changes
- Add a small set of known-but-unsupported S3 bucket-config subresource query
  params and intercept them at the S3 dispatch root, addressed at the bucket
  root (`key == ""`), across all methods (GET/PUT/POST/DELETE).
- Interception returns S3 error `501` code `NotImplemented` (bounded write, no
  store dependency) before any object/list/multipart routing.
- Supported subresources are untouched: `?location`, `list-type`,
  `uploads`, `uploadId`/`partNumber`, `delete`, and the ListObjectsV2 pagination
  params continue to route as before.

## Non-Goals
- Object-level subresource variants (`GET /bucket/key?tagging` =
  GetObjectTagging, `?versionId` versioning of individual objects) — Tier P4
  of #820, out of scope for this increment.
- Any actual implementation of versioning/ACL/policy/CORS/website/lifecycle/tagging.
