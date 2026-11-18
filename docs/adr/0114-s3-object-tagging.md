# 0114-s3-object-tagging

## Status
Proposed

## Confidence
Low

## Context
Object tagging answered `501 NotImplemented` and `x-amz-tagging` on a PUT was
ignored. Tags are load-bearing in real pipelines: lifecycle rules filter on them,
cost allocation attributes by them, and `aws s3api put-object-tagging` is an
everyday command.

## Decision
- Tag Subresource: `GET`, `PUT` and `DELETE` on `?tagging` SHALL operate on the object's tag set.
- Tag Rules: The tag set SHALL be validated before it is stored.
- Header Form: `x-amz-tagging` on a plain PUT SHALL be stored as the object's tag set.
- Storage Shape: Tags SHALL be persisted in the S3 metadata sidecar in the S3 wire form.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1375
- PR: 
- Spec: `openspec/changes/s3-object-tagging/`
- Blog: 

