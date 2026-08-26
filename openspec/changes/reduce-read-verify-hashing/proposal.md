# Change: reduce-read-verify-hashing — immutable-CAS verified-cache read path (Win1)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/950
- https://github.com/alsotoes/momo/issues/948 (baseline parent)

## Why

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

## What

Introduce a **`ReadVerifier` seam (Rule 74)** that replaces the raw
`VerifyOnRead bool` decision at the read wrap-point with a policy-selected
verifier:

1. `everyReadVerifier` — the current behavior (always wrap + full SHA-256).
   **Default; zero behavior change.**
2. `verifiedCache` — serves a blob WITHOUT re-hashing once it is in a trusted
   set; a blob enters trust ONLY after a successful full SHA-256 verification in
   this process, on either:
   - a successful streaming verify at read EOF (`verifyingReader` path), or
   - a successful `scrubBlob` pass (`integrity.go:195`, digest==hash).
   Trusted blobs read via plain `io.Copy` (no hasher) — no allocation slip.
3. Seam is constructor-injected (functional option on `newCASStore`), policy
   chosen declaratively; `VerifyOnRead bool` remains as the compatibility knob
   mapping to `everyReadVerifier` on / `verifiedCache` off.
4. **Trusted set** is in-process (`map[string]struct{}`, mutex-guarded); reset on
   restart → safe re-verify once per boot. Not persisted (no schema change).

## Out of scope

- Changing the content address or hash (stays SHA-256; the object key is the
  hash — protocol-invariant).
- Changing write-path verification (still single-pass as today).
- Removing/weakening `StartScrub` scrub coverage.
- Any networked pprof or dynamic plugin (Rule 74/75).

## Goals / Non-Goals

- **Goals:** cut repeated full-object read hashing to ~raw disk-read cost for
  already-trusted blobs; keep integrity (never trust an unverified blob;
  immutability makes trust permanent; scrub re-catches rot); benchmark-provable;
  default behavior unchanged.
- **Non-Goals:** trust without prior in-process verification; persistence;
  changing the hash; touching S3 SigV4 or write path.

## Success Criteria

- `ReadVerifier` seam present; `verifiedCache` and `everyReadVerifier` both
  compiled-in; default maps to current behavior.
- Trust only ever granted after a real successful full verification (read EOF or
  scrub).
- New benchmark: repeated read of a trusted blob approaches raw disk read
  (~2400 MB/s); cold `BenchmarkReadVerify` NOT regressed.
- `make test` green; PR ships `Resolves #950`; three-dot diff scoped to the seam
  files + this OpenSpec set.
