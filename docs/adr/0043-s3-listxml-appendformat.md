# 0043-s3-listxml-appendformat

## Status
Proposed

## Confidence
Low

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
- **Code**: Partial
- **Tests**: Partial
- **Docs**: Partial
- **Blog post**: docs/blog/posts/036-s3-listxml-appendformat-optimization.md

## References
- Issue: #900
- PR: 
- Spec: `openspec/changes/s3-listxml-appendformat/`
- Blog: docs/blog/posts/036-s3-listxml-appendformat-optimization.md

