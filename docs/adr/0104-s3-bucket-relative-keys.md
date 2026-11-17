# 0104-s3-bucket-relative-keys

## Status
Accepted

## Confidence
High

## Context
A path-style PUT stored the object under the **full path** (including the bucket
segment) whenever no `[storage] s3_bucket` was configured, while every read path
resolves the key by stripping the first segment. A real S3 client therefore
could not read back what it had just written: `put-object` returned 200 and
`head-object`/`get-object` answered 404, with `list-objects-v2` showing the
bucket-prefixed name.

Loading the metadata also used an empty object name as its "not parsed yet"
sentinel, so a request that legitimately parses to an empty key made the daemon
block waiting for another request on the same connection.

## Decision
- Store The Bucket-Relative Key: The gateway SHALL store an uploaded object under the bucket-relative key when the request path carries a bucket segment.
- Explicit Parse State: The gateway SHALL track "metadata already parsed" with explicit state rather than inferring it from an empty object name.
- One Naming Rule: Every parse path SHALL apply the same naming rule, including the `OPTIONS`-then-`PUT` fallback.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1348
- PR: 
- Spec: `openspec/changes/s3-bucket-relative-keys/`
- Blog: 

