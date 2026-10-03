# 0056-bolt-aws-chunked-zero-alloc

## Status
Accepted

## Confidence
High

## Context
Every `aws-chunked` (streaming SigV4) upload body is framed as a sequence of
chunks, each preceded by a header line of the form
`hex-size[;chunk-signature=<sig>][;ext=v]`. `parseAWSChunkHeader` parses that
line on the S3 ingest hot path — once per chunk.

The current implementation uses `strings.Split(line, ";")`, which allocates a
slice header plus a backing array for the split parts on every call, and
`strings.TrimPrefix` to strip the `chunk-signature=` field, which allocates a
new string on every match. For a body split into ~64 KiB chunks (the aws-cli /
SDK default), that is two avoidable heap allocations per chunk — pure GC
pressure that scales with object size.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/051-bolt-aws-chunked-zero-alloc.md

## References
- Issue: #1113
- PR: 
- Spec: `openspec/changes/bolt-aws-chunked-zero-alloc/`
- Blog: docs/blog/posts/051-bolt-aws-chunked-zero-alloc.md

