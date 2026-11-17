# 0109-s3-multipart-upload-expiry

## Status
Proposed

## Confidence
Low

## Context
An incomplete multipart upload held two kinds of state with no expiry: an
in-memory session in the package-level `uploads` map, and a per-upload spill
directory holding every uploaded part (#1358). A client that uploads parts and
never completes or aborts — a crashed tool, an interrupted `aws s3 cp`, a retried
pipeline — leaked both for the life of the daemon. A daemon killed mid-upload
leaked the directory with no session at all.

Real S3 bounds this with an `AbortIncompleteMultipartUpload` lifecycle rule; the
gateway has no lifecycle support for it and no other bound.

## Decision
- Expired Sessions Are Discarded: An incomplete multipart session older than the configured TTL SHALL be removed along with its spooled parts.
- Orphaned Spills Are Swept: A spill directory that no live session owns SHALL be removed once it is older than the TTL.
- Disableable: `multipart_upload_ttl = 0` SHALL disable reaping, and a negative or unparseable value SHALL be refused at config load.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1361
- PR: 
- Spec: `openspec/changes/s3-multipart-upload-expiry/`
- Blog: 

