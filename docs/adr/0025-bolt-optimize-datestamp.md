# 0025-bolt-optimize-datestamp

## Status
Accepted

## Confidence
High

## Context
When constructing AWS SigV4 requests, both a full timestamp (`20060102T150405Z`) and a datestamp (`20060102`) are required. Currently, the code calls `time.Format` twice, passing different format strings to `time.Now().UTC()`. `time.Format` dynamically parses the format string and allocates a new string on the heap for each call. Calling it twice sequentially creates unnecessary garbage collection pressure and CPU overhead on hot paths for request signing.

## Decision


## Consequences
- Reduced heap allocations per request on S3 communication layers.
- Lower GC pressure and CPU overhead.

## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**:

## References
- Issue: #1065
- PR:
- Spec: `openspec/changes/bolt-optimize-datestamp/`
- Blog:
