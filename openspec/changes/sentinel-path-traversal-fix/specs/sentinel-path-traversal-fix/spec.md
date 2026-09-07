> GitHub Issue URL: https://github.com/alsotoes/momo/issues/997

# sentinel-path-traversal-fix Specification

## Purpose

Enforce that incoming file hash metadata is validated for path traversal
characters immediately upon extraction from the raw wire buffer, strictly prior
to any log sanitization or string manipulation, and ensure connections are
closed upon panic recovery.

## ADDED Requirements

### Requirement: pre-sanitization path traversal validation

`MomoTCPCommunicator.ReceiveMetadata` and `MomoQUICCommunicator.ReceiveMetadata`
SHALL validate the extracted `rawHash` string using `common.HasPathTraversalChars`
and empty-string checks BEFORE passing `rawHash` to `common.SanitizeLog`.

#### Scenario: traversal token in raw hash
- **GIVEN** an incoming metadata buffer with a hash containing traversal characters
  (e.g., `..`, `/`, `\`)
- **WHEN** `ReceiveMetadata` parses the wire frame
- **THEN** `ReceiveMetadata` returns an error wrapping `syscall.EBADMSG` without
  mutating the string through `SanitizeLog`

#### Scenario: valid hash
- **GIVEN** a legitimate 64-character SHA-256 hash string
- **WHEN** `ReceiveMetadata` parses the wire frame
- **THEN** validation succeeds, `SanitizeLog` is applied for logging safety, and
  metadata decoding proceeds normally

### Requirement: panic recovery connection cleanup

The deferred panic recovery handlers in `ReceiveMetadata` SHALL call `m.Close()`
when `m != nil` before returning `syscall.EIO`, preventing zombie socket
accumulation.

#### Scenario: unexpected panic during metadata reception
- **GIVEN** a panic during `ReceiveMetadata` execution
- **WHEN** the deferred recovery executes
- **THEN** the underlying connection is closed and `syscall.EIO` is returned
