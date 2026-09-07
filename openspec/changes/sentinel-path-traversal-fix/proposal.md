# Change: Sentinel — Validate Raw Wire Hash Before Log Sanitization in ReceiveMetadata

**Related Issues:**
- https://github.com/alsotoes/momo/issues/997 (tracking)

## Why

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

## What Changes

- In `src/transport/momo_tcp.go` (`ReceiveMetadata`):
  - In panic recovery defer, call `m.Close()` if `m != nil` to avoid zombie sockets.
  - Perform `rawHash == "" || common.HasPathTraversalChars(rawHash)` validation
    directly on the raw wire-extracted string *before* calling `common.SanitizeLog`.
- In `src/transport/momo_quic.go` (`ReceiveMetadata`):
  - In panic recovery defer, call `m.Close()` if `m != nil`.
  - Perform `rawHash == "" || common.HasPathTraversalChars(rawHash)` validation
    directly on `rawHash` *before* calling `common.SanitizeLog`.
- In `src/transport/momo_tcp_test.go`:
  - Add `TestMomoTCPReceiveMetadata_RejectsPathTraversal` asserting that path
    traversal patterns in `rawHash` (`..`, `/`, `\`, empty hash) fail closed with
    `syscall.EBADMSG`.

## Non-Goals

- No protocol frame format changes.
- No changes to `common.HasPathTraversalChars` component check logic.

## Impact

- **Security:** Fail-closed boundary defense directly on raw wire buffers.
- **Correctness:** Malicious traversal tokens are rejected before any string mutation occurs.
