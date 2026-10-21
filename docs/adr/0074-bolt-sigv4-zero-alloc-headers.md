# 0074-bolt-sigv4-zero-alloc-headers

## Status
Proposed

## Confidence
High

## Context
`buildCanonicalHeaders` (`src/transport/sigv4.go`) builds the canonical-headers
block of the AWS SigV4 canonical request on **every authenticated S3 request** —
the hottest authorization path in the gateway. Two avoidable allocations happen
per call:

1. `strings.Split(signedHeadersStr, ";")` allocates a slice header plus a
   backing array for the header-name list.
2. `req.Header.Get(h)` routes through `net/textproto.CanonicalMIMEHeaderKey`,
   which allocates a canonicalized copy of the key string on each lookup.

Both are pure GC pressure on a latency-sensitive, per-request path. `⚡ Bolt`
(STANDARDS.md §1, Rule 19) requires zero-allocation hot paths.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Partial
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/057-bolt-sigv4-zero-alloc-headers.md

## References
- Issue: #1195
- PR: 
- Spec: `openspec/changes/bolt-sigv4-zero-alloc-headers/`
- Blog: docs/blog/posts/057-bolt-sigv4-zero-alloc-headers.md

