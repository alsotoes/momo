# 0043-s3-listxml-appendformat

## Status
Accepted

## Confidence
High

## Context
Eliminate per-element heap allocation in `FormatListObjectsV2XML` by rendering
`<LastModified>` with `time.AppendFormat` into a stack-allocated buffer instead
of `formatLastModified`'s `time.Format` (which allocates a heap string each
call).

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/036-s3-listxml-appendformat-optimization.md

## References
- Issue: #900
- PR: 
- Spec: `openspec/changes/s3-listxml-appendformat/`
- Blog: docs/blog/posts/036-s3-listxml-appendformat-optimization.md

