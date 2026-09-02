# 0037-reduce-read-verify-hashing

## Status
Accepted

## Confidence
High

## Context
Phase-0 baseline (#948/#949) proves read-path SHA-256 is the dominant storage
cost: `BenchmarkReadVerify/1MiB` ≈ 3.35 ms/op (~313 MB/s) vs
`BenchmarkLocalWrite/1MiB` ≈ 0.44 ms/op (~2408 MB/s). Every read re-hashes the
**full object** through `verifyingReader` (`storage.go:316-323`) just to confirm
the content-address.

Critical data from #948: `go tool pprof` shows 100% CPU in
`crypto/internal/fips140/sha256.blockAVX2` — Go's stdlib **already** uses AVX2
SIMD assembly. So swapping the hash primitive (a naive "SIMD SHA-256" win) has
thin upside on amd64. The real win is to **stop redundant re-hashing of blobs
already proven good**, not to hash faster.

Because blobs are content-addressed and immutable, a blob that has been fully
SHA-256-verified once against its address key **cannot change afterward**. A
per-blob verified set therefore lets later reads skip re-hashing with zero
integrity loss, while the existing background scrub re-catches silent disk rot.

## Decision
- ReadVerifier seam: A `ReadVerifier` MUST replace the raw boolean `VerifyOnRead` gate at the read wrap-point (`storage.go` around the `if s.VerifyOnRead { ... }` block that wraps `GetBlob` output in `verifyingReadCloser`) with a policy-chosen verifier.
- trust is earned, immutable: A blob MAY be added to the verified set ONLY as a consequence of a successful full SHA-256 verification of its bytes against its content-address key, from either source: read-path streaming verify at EOF, or a `scrubBlob` digest match (`integrity.go:195`).
- integrity preservation: Skipping read re-hash MUST NOT reduce scrub coverage and MUST NOT ever trust a blob that was not fully verified against its content-address in process.
- benchmark gate (Rule 73): Performance MUST be benchmark-proven and must not regress the cold path. ## Success Criteria - `ReadVerifier` seam + two impls in `src/storage`; default = current behavior. - Trusted-set mutex-guarded, in-process, empty on boot. - `make test` green; new/UPDATED benchmark showing trusted-path speedup and cold-path non-regression. - PR ships `Resolves #950`; three-dot diff scoped to seam + tests + this set.

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/reduce-read-verify-hashing/
- Blog: docs/blog/posts/...md
