# 0090-zero-alloc-s3-listing

## Status
Proposed

## Confidence
Low

## Context
Formatting S3 list responses dynamically with `xml.MarshalIndent` and `time.Format` inside large array serialization loops creates significant garbage collection pressure due to heavy heap allocation overhead per element. This degrades performance on the S3 metadata retrieval hot path.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**:

## References
- Issue: #1285
- PR:
- Spec: `openspec/changes/zero-alloc-s3-listing/`
- Blog:
