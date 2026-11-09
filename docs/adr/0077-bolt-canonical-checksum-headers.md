# 0077-bolt-canonical-checksum-headers

## Status
Proposed

## Confidence
Low

## Context
The S3 gateway probes four checksum headers on every request to discover whether a
client supplied an integrity checksum. The probe built each header name at
runtime (`"x-amz-checksum-" + algo`) and passed it to `http.Header.Get`, which
canonicalizes its argument. That allocated a string and re-ran canonicalization
four times per request, even when the request carried no checksum at all —
GC pressure on the hottest ingest path for no benefit.

## Decision
- Zero-Allocation Header Probe: The gateway MUST resolve a request's checksum algorithm without allocating memory per request, including when no checksum header is present.
- Behaviour Preservation: The probe MUST keep the established algorithm precedence and error semantics.
- Measurable Savings: A benchmark MUST demonstrate the allocation difference so the optimization can be re-verified.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: docs/blog/posts/067-bolt-checksum-header-lookups.md

## References
- Issue: #1275
- PR: 
- Spec: `openspec/changes/bolt-canonical-checksum-headers/`
- Blog: docs/blog/posts/067-bolt-checksum-header-lookups.md

