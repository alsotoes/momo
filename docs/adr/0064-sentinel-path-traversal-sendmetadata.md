# 0064-sentinel-path-traversal-sendmetadata

## Status
Proposed

## Confidence
Low

## Context
Path traversal detection in metadata transport methods (`SendMetadata`) evaluated `strings.Split(path, "/")` parts individually, which incorrectly strips out slashes and fails to detect some payloads.
Malicious clients could send a crafted file name in metadata to write files outside the expected directory structure (Path Traversal/Arbitrary File Write).

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
- Issue:
- PR:
- Spec: `openspec/changes/sentinel-path-traversal-sendmetadata/`
- Blog:
