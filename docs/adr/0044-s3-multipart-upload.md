# 0044-s3-multipart-upload

## Status
Proposed

## Confidence
Medium

## Context
AWS S3 clients (aws-cli, aws-sdk-go-v2, boto3) automatically switch to multipart upload for objects above a size threshold (8 MB default). momo's S3 gateway implemented only monolithic `PUT /bucket/key`. Multipart operations were entirely unimplemented: any object uploaded over the SDK threshold failed. This broke large-file support — a core AWS S3 use case.

## Decision
- Multipart upload endpoint interception: The gateway SHALL intercept all six multipart REST endpoints in `HandshakeServer` and handle them end-to-end (returning `ErrRequestHandled`). Upload state SHALL be tracked in memory.
- In-memory state tracking: The gateway SHALL track multipart upload state using a `map[string]*multipartUpload` protected by `sync.Mutex`. Each upload SHALL store the bucket, key, creation time, and an ordered list of parts (part number, ETag, body data). On `CompleteMultipartUpload`, the upload SHALL be removed from the map after assembly.
- Store integration: On `CompleteMultipartUpload`, the assembled object SHALL be stored through the standard `store.Put()` path so the object flows through the normal momo CAS/persistence/replication pipeline.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/s3-multipart-upload/
- Blog: docs/blog/posts/...md
