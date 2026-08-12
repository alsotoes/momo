# Tasks: Signed Payload Outbound SigV4 + Honest SSE Negotiation (issue #776)

## 1. Phase 1: Outbound SIGNED_PAYLOAD (`S3BlobStore.PutBlob`)
- [x] 1.1 Spool the body to a bounded temp file while computing its SHA-256 (`io.MultiWriter(spill, hasher)`), then `Seek(0)` and set `req.ContentLength = written`.
- [x] 1.2 Sign with `X-Amz-Content-Sha256: <sha256(content)>` (SIGNED_PAYLOAD) so the SigV4 signature binds the content; remove the now-unused `countingReader`.
- [x] 1.3 Reject oversized blobs with `EFBIG` before any upload (no wasted upload + `DeleteBlob` cleanup); always close/remove the spool file on every path.
- [x] 1.4 Regression tests with a SigV4-verifying mock S3 server that recomputes the signature over the **actual received body**:
  - [x] 1.4.1 PutBlob sends the real content hash, not the `UNSIGNED-PAYLOAD` literal.
  - [x] 1.4.2 A tampered body is rejected (declared hash != actual body hash) with 403.
  - [x] 1.4.3 An explicit `UNSIGNED-PAYLOAD` request still verifies and is accepted (presigned compatibility).

## 2. Phase 2: Honest SSE negotiation (gateway `S3Communicator`)
- [x] 2.1 Add `validateSSEHeaders(req)`: SSE-C customer-key headers → `400 InvalidRequest` (`EINVAL`); `aws:kms` / kms-key-id → `501 NotImplemented` (`ENOTSUP`); unknown algorithm → `400 InvalidArgument` (`EINVAL`); `AES256` → accepted. Never stores a customer key.
- [x] 2.2 Call `validateSSEHeaders` at the top of the PUT branch in `HandshakeServer` (before CopyObject/CreateBucket/upload dispatch) so every PUT variant is covered.
- [x] 2.3 Add `X-Amz-Server-Side-Encryption` to `s3standardHeaders` so an accepted AES256 is captured, persisted via S3Meta, and echoed on GET/HEAD (peer-forwarded metadata carries it too).
- [x] 2.4 Accept and document `x-amz-sdk-checksum-algorithm` (no rejection — aws-cli v2 default; momo does not compute AWS additive checksums).

## 3. Phase 3: Tests
- [x] 3.1 Gateway PUT: AES256 captured for persistence; SSE-C (all three customer headers), SSE-KMS (algorithm + key-id), and unknown algorithm rejected with the right status/Code/POSIX error.
- [x] 3.2 GET echoes the stored `x-amz-server-side-encryption: AES256` header.
- [x] 3.3 Unsigned aws-chunked streaming PUT (`STREAMING-UNSIGNED-PAYLOAD-TRAILER`) is still de-framed and accepted (issue #773 posture preserved).
- [x] 3.4 Full transport suite passes under `-race`; storage suite passes with `goleak`.

## 4. Docs
- [x] 4.1 Add `openspec/changes/signed-payload-and-sse/` (proposal, spec, tasks) with `Resolves #776` links.
- [x] 4.2 Update `docs/PROTOCOL.md` (S3 section): outbound SIGNED_PAYLOAD, inbound UNSIGNED tolerance, SSE negotiation matrix, checksum-algorithm posture.

## 5. Validation
- [ ] 5.1 Run `gofmt`, `go vet`, and the full per-module test suites (`src/root`, `common`, `transport` incl. `-race`, `client`, `server`, `storage`, `metrics`, `crypto`, `p2p`).
- [ ] 5.2 Commit (pre-commit hook syncs `docs/PERFORMANCE.md` / `.github/data/benchmark_history.csv`), Rule 58 branch check, push.
- [ ] 5.3 Open PR with `Resolves #776`, wait for checks, address the `github-actions` review, merge `--merge --delete-branch`, close the issue, run the Rule 71 master gate.
