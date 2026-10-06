# 0057-bolt-etag-matches-zero-alloc

## Status
Accepted

## Confidence
High

## Context
`etagMatches` parses a comma-separated entity-tag list from the `If-Match` /
`If-None-Match` request headers on the S3 conditional-request hot path. The
current implementation uses `strings.Split(header, ",")`, which allocates a
slice header plus a backing array for the split parts on every call. For
clients that send conditional requests frequently (S3 GET/HEAD with
`If-None-Match`), this is one avoidable heap allocation per request — pure GC
pressure on a latency-sensitive path.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/052-bolt-etag-matches-zero-alloc.md

## References
- Issue: #1133
- PR: 
- Spec: `openspec/changes/bolt-etag-matches-zero-alloc/`
- Blog: docs/blog/posts/052-bolt-etag-matches-zero-alloc.md

