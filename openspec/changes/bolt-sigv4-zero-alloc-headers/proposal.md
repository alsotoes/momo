# Change: Bolt — zero-allocation SigV4 canonical header construction

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1195 (tracking)

## Why

`buildCanonicalHeaders` (`src/transport/sigv4.go`) builds the canonical-headers
block of the AWS SigV4 canonical request on **every authenticated S3 request** —
the hottest authorization path in the gateway. Two avoidable allocations happen
per call:

1. `strings.Split(signedHeadersStr, ";")` allocates a slice header plus a
   backing array for the header-name list.
2. `req.Header.Get(h)` routes through `net/textproto.CanonicalMIMEHeaderKey`,
   which allocates a canonicalized copy of the key string on each lookup.

Both are pure GC pressure on a latency-sensitive, per-request path. `⚡ Bolt`
(STANDARDS.md §1, Rule 19) requires zero-allocation hot paths.

## What Changes

- `buildCanonicalHeaders` (`src/transport/sigv4.go`):
  - Parse the `SignedHeaders` list with a `strings.IndexByte` scan into a
    stack-allocated `[16]string` buffer (no `strings.Split`).
  - Look up each header value by iterating `req.Header` with `strings.EqualFold`
    instead of `req.Header.Get` (no `CanonicalMIMEHeaderKey` allocation).
  - Return `(string, error)`; the existing caller `buildCanonicalRequest`
    already propagates the error.
- New helpers `parseSignedHeaders` and `getCanonicalHeaderValue` keep each
  function under the cognitive-complexity budget (STANDARDS.md).
- **Sentinel bounds** (Rule 32): the signed-header count is bounded by the
  stack buffer capacity — an oversized list fails closed with `EBADMSG`.
- **Rule 37/43**: panic recovery returns `syscall.EIO` instead of crashing.
- `src/transport/sigv4_golden_test.go` (new): a golden-equivalence oracle that
  asserts byte-identical canonical output against the legacy implementation for
  every AWS-conformant input, plus explicit tests for the two intentional
  divergences.
- Append a `.jules/bolt.md` learning entry (Rule 44 append-only).

## Non-Goals

- No change to the SigV4 signing/verification decision logic, the credential
  scope, or `buildCanonicalRequest`'s other components.
- No wire/protocol or configuration changes.
- Other `strings.Split` / `Header.Get` sites are out of scope.

## Impact

- Behavior is byte-identical for all AWS-conformant requests (verified by the
  golden test). Two intentional divergences, both fail-closed, are documented:
  - A signed header absent from the request now returns `EBADMSG` instead of
    silently contributing an empty value (both paths fail authentication).
  - Empty segments in a malformed `SignedHeaders` list are skipped instead of
    emitting a stray `:\n` entry.
- Removes 2 heap allocations per authenticated S3 request on the auth path.
