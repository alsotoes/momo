# 0049-sentinel-path-traversal-fix

## Status
Accepted

## Confidence
High

## Context
In `ReceiveMetadata()` for both Momo-TCP (`src/transport/momo_tcp.go`) and
Momo-QUIC (`src/transport/momo_quic.go`), path traversal validation was
previously performed *after* the wire buffer was passed to `common.SanitizeLog`.
Because sanitization functions mutate strings (stripping null bytes, control
characters, or non-printable bytes), a malicious payload could theoretically
bypass traversal detection if the traversal token only materialized or shifted
after sanitization.

Furthermore, per Rule 43 (Explicit Resource Cleanup on Panic), transport
`ReceiveMetadata()` panic recovery handlers must explicitly close the active
connection (`m.Close()`) to prevent resource exhaustion and lingering zombie
sockets.

## Decision
- pre-sanitization path traversal validation: `MomoTCPCommunicator.ReceiveMetadata` and `MomoQUICCommunicator.ReceiveMetadata` SHALL validate the extracted `rawHash` string using `common.HasPathTraversalChars` and empty-string checks BEFORE passing `rawHash` to `common.SanitizeLog`.
- panic recovery connection cleanup: The deferred panic recovery handlers in `ReceiveMetadata` SHALL call `m.Close()` when `m != nil` before returning `syscall.EIO`, preventing zombie socket accumulation.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/sentinel-path-traversal-fix/
- Blog: docs/blog/posts/...md
