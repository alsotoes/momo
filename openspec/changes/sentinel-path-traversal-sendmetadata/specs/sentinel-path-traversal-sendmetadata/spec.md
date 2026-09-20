# Spec: Fix Path Traversal via string splitting in SendMetadata

## Requirement
- **Requirement:** `wireName` input in `SendMetadata` (for TCP, QUIC, and S3 protocols) SHALL be validated against path traversal by applying `path.Clean()` and asserting that the resulting clean path does NOT equal `.`, `..`, and does NOT start with `../` or `/`.
- **Requirement:** The `SendMetadata` input SHOULD ALSO be length-validated against the protocol limit (64 bytes) before path cleaning logic runs, to prevent resource exhaustion.
