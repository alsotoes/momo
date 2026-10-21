# Tasks: Bolt — zero-allocation SigV4 canonical header construction

## Implementation

- [x] `parseSignedHeaders` — `strings.IndexByte` scan into a stack `[16]string` buffer (no `strings.Split`)
- [x] `getCanonicalHeaderValue` — `strings.EqualFold` iteration over `req.Header` (no `CanonicalMIMEHeaderKey`)
- [x] `buildCanonicalHeaders` returns `(string, error)`; caller `buildCanonicalRequest` propagates it
- [x] Rule 32 bound: signed-header count capped by buffer capacity → `EBADMSG`
- [x] Rule 37/43: panic recovery returns `syscall.EIO`
- [x] Append `.jules/bolt.md` learning entry (Rule 44)

## Testing

- [x] `TestBuildCanonicalHeaders_GoldenEquivalence` — byte-identical vs the legacy oracle across conformant inputs
- [x] `TestBuildCanonicalHeaders_MissingHeader` — absent signed header fails closed (`EBADMSG`)
- [x] `TestBuildCanonicalHeaders_NonLowercaseHost` — documents the legacy empty-host bug vs the new fail-closed path
- [x] `TestBuildCanonicalHeaders_EmptySegmentsSkipped` — malformed `;;`/trailing `;` handled
- [x] `TestBuildCanonicalHeaders_HeaderCountBound` — oversized list rejected (Rule 32)
- [x] `TestParseSignedHeaders` — table decomposed to satisfy the complexity budget
- [x] Full transport suite passes (`go test ./src/transport/` — 358+ tests)
- [x] `go build`, `go vet`, `gofmt`, `make lint` clean

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rule 77/78)
- [x] Blog post (Rule 76)
- [ ] PR links tracking issue #1195 (Rule 11)
