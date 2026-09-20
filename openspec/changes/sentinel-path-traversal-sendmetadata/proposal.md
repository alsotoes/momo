# Proposal: Fix Path Traversal via string splitting in SendMetadata

## Context
Path traversal detection in metadata transport methods (`SendMetadata`) evaluated `strings.Split(path, "/")` parts individually, which incorrectly strips out slashes and fails to detect some payloads.
Malicious clients could send a crafted file name in metadata to write files outside the expected directory structure (Path Traversal/Arbitrary File Write).

## Decision
Use `path.Clean()` to validate the path and reject if the result equals `.`, `..`, or starts with `../` or `/`.

## Consequences
Invalid paths that contain path traversals will be rejected at the transport layer, effectively preventing path traversal vulnerabilities when processing metadata.

## Alternatives Considered
- None
