> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1231

# sigv4-parsing-bound-audit Specification

## Purpose

Keep the SigV4 authentication parsers provably bounded and align their failure
reporting with the project's POSIX error convention, while documenting that the
defense already lives at the handshake read layer rather than in redundant
per-loop counters.

## ADDED Requirements

### Requirement: SigV4 parsers operate on bounded input

The SigV4 authentication parsers SHALL operate only on input bounded by the
handshake read limit (`LimitedConnReader.SetLimit`, 65536 bytes) and by the
existing canonical-header value (4096 bytes) and signed-header count (16) caps.

#### Scenario: oversized Authorization header
- **WHEN** a client sends a handshake whose Authorization header pushes the
  handshake read past 64 KiB
- **THEN** `HandshakeServer` fails with a wrapped `syscall.EBADMSG` before any
  SigV4 parser is invoked.

#### Scenario: credential scope with missing separators
- **WHEN** a `Credential=` component lacks one of the four scope separators
- **THEN** `parseSigV4AuthHeader` returns `syscall.EBADMSG`.

### Requirement: parseSigV4AuthHeader reports POSIX errors

`parseSigV4AuthHeader` SHALL return `(sigV4Components, error)` and return
`syscall.EBADMSG` for a missing scheme, missing components, or an empty access
key, replacing the previous bare boolean result.

#### Scenario: malformed header rejected uniformly
- **WHEN** any malformed Authorization header is supplied
- **THEN** the returned error is `syscall.EBADMSG` and callers treat it as an
  authentication failure (unchanged user-visible behavior).
