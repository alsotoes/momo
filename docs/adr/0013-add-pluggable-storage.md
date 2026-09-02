# 0013-add-pluggable-storage

## Status
Accepted

## Confidence
High

## Context
The storage backend was hardcoded to the local filesystem. To support NFS, S3-compatible APIs, and raw block devices, the blob storage layer must be pluggable. Default behavior (local path) must be preserved when unconfigured.

## Decision
- Pluggable Storage Backend: The system SHALL support configurable storage backends selected via the `backend` config field in the `[storage]` section. The default backend (`local`) SHALL preserve existing behavior exactly. ## MODIFIED Requirements
- Content-Based File Addressing: The system SHALL store and retrieve files based on a cryptographic hash of their content, using the pluggable `BlobStore` interface for raw blob bytes and local bbolt for metadata.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-pluggable-storage/
- Blog: docs/blog/posts/...md
