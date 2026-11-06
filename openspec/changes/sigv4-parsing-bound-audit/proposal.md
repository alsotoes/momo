# Change: SigV4 Parsing Bound Audit — POSIX Error Refactor + Regression Tests

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1231 (tracking)

## Why

PR #1230 reported a CRITICAL unbounded-iteration vulnerability in the SigV4
authentication parsers (`parseSigV4AuthHeader`, `parseSignedHeaders`,
`getCanonicalHeaderValue`, `buildCanonicalQueryString`) and claimed to add
"hard caps of 64 on loop iterations".

Audit of `origin/master` found the claim unfounded:

- `S3Communicator.HandshakeServer` caps the entire handshake read at 64 KiB via
  `LimitedConnReader.SetLimit(65536)` (Rule 24), so every downstream parser is
  bounded by construction.
- `parseSigV4AuthHeader` consumes at least one byte per outer iteration, so the
  loop terminates and its cost is linear in an input that cannot exceed 64 KiB.
- Canonical header values were already capped at 4096 bytes and signed headers at
  16 entries in earlier merges.

No loop-iteration cap is required. The one genuine improvement left in the PR was
a signature refactor (`bool` → POSIX `error`), which the reviewers preferred but
which changes no behavior. This change ships that refactor plus regression tests
that pin the actual bounds, so a future widening of the handshake read limit
cannot silently reopen the surface.

## What Changes

- `parseSigV4AuthHeader` returns `(sigV4Components, error)` with `syscall.EBADMSG`
  on malformed input instead of a bare `bool`, matching the POSIX error convention
  used by the other SigV4 helpers.
- Call sites in `S3Communicator.HandshakeServer` and `verifySigV4Signature`
  consume the error.
- New tests record the bound chain: valid headers parse, malformed credentials
  return `EBADMSG`, and an Authorization header beyond the 64 KiB handshake limit
  is rejected by the existing read cap before any parser runs.
- No iteration caps are added: they would be unreachable dead code.

## Impact

- Affected specs: `sigv4-parsing-bound-audit` (new)
- Affected code: `src/transport/sigv4.go`, `src/transport/s3_communicator.go`
- Behavior: unchanged. Malformed headers still fail authentication; the failure
  path now carries `EBADMSG` instead of a boolean.
