# Tasks: S3 Integrity Checksums (#820 Tier P2)

## 1. Phase 1: gateway parsing + hashing helpers
- [ ] `newChecksumHasher(algo)` — CRC32/CRC32C/SHA1/SHA256 digest constructors
- [ ] `isSupportedChecksum`, `checksumHeaderFor`, `normalizeChecksumAlgorithm`
- [ ] `parseChecksum(req)` — resolve/validate `x-amz-checksum-*`; deterministic reject

## 2. Phase 2: streaming upload verification (single-part + aws-chunked)
- [ ] Arm checksum state in `HandshakeServer` PUT path (algo/expected/hasher)
- [ ] Tee payload digest inside `S3Communicator.Read`
- [ ] `FinalizeS3Checksum` verifier + call from `getFile`; mismatch → `400 BadDigest` + delete

## 3. Phase 3: multipart verification
- [ ] Verify assembled bytes in `handleCompleteMultipartUpload` before `store.Put`
- [ ] Persist multipart checksum via `putS3Meta`

## 4. Phase 4: GET checksum mode + persistence
- [ ] `collectS3Headers` capture of `x-amz-checksum-*` + `x-amz-checksum-algorithm`
- [ ] GET `x-amz-checksum-mode:ENABLED` compute+return for unchecksummed objects

## 5. Phase 5: docs + tests
- [ ] COMPATIBILITY.md checksum row graded (remove from "not implemented" note)
- [ ] Tests: single-part bad→BadDigest, single-part good→echo, aws-chunked,
      multipart bad→BadDigest, unknown-algo→InvalidRequest, GET checksum-mode
- [ ] `go build/vet/test ./...`, `go test -race`, `go work vendor` no-diff
