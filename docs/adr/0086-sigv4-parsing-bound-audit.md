# 0086-sigv4-parsing-bound-audit

## Status
Accepted

## Confidence
High

## Context
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

## Decision
- SigV4 parsers operate on bounded input: The SigV4 authentication parsers SHALL operate only on input bounded by the handshake read limit (`LimitedConnReader.SetLimit`, 65536 bytes) and by the existing canonical-header value (4096 bytes) and signed-header count (16) caps.
- parseSigV4AuthHeader reports POSIX errors: `parseSigV4AuthHeader` SHALL return `(sigV4Components, error)` and return `syscall.EBADMSG` for a missing scheme, missing components, or an empty access key, replacing the previous bare boolean result.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1231
- PR: 
- Spec: `openspec/changes/sigv4-parsing-bound-audit/`
- Blog: 

