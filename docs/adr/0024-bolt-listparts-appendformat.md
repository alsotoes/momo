# 0024-bolt-listparts-appendformat

## Status
Accepted

## Confidence
High

## Context
`S3Communicator.handleListParts` renders the `ListPartsResult` XML response by
formatting the upload's last-modified timestamp once and writing it into every
`<Part>` element. The current implementation calls `time.Now().UTC().Format(time.RFC3339)`,
which returns a dynamically allocated string — so every S3 `ListParts` request
pays one heap allocation (24 B) just to produce the timestamp. Under workloads
that poll multipart upload progress (the common aws-cli / SDK pattern), this
allocation lands on a repeated hot request path and adds GC pressure.

The standard-library idiom `time.AppendFormat` writes the formatted time
directly into a stack-allocated `[32]byte` buffer, eliminating the heap
allocation entirely — the same pattern already shipped for
`CopyObjectResult` (`bolt-s3-copyresult-time-alloc`) and HTTP `Last-Modified`
headers (`bolt-http-lastmodified-appendformat`).

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/048-bolt-listparts-appendformat.md

## References
- Issue: #1063
- PR: 
- Spec: `openspec/changes/bolt-listparts-appendformat/`
- Blog: docs/blog/posts/048-bolt-listparts-appendformat.md

