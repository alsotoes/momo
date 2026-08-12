# Tasks: S3 aws-chunked Streaming Payload Support (issue #773)

## 1. Phase 1: aws-chunked De-framer
- [x] 1.1 Create `src/transport/aws_chunked.go` with `awsChunkedReader` (an `io.Reader` over a `*bufio.Reader`) implementing the wire grammar: `<hex-size>[;chunk-signature=<64hex>]\r\n<raw data>\r\n` … terminating `0[;chunk-signature=<sig>]\r\n\r\n` plus optional trailer block.
- [x] 1.2 Bound chunk header lines (≤ 1 KiB), chunk data size (≤ AWS 8 MiB cap), and total decoded bytes (`common.MaxFileSize`).
- [x] 1.3 Stream: compute decoded SHA-256 and decoded byte count as bytes pass through; expose them after EOF.
- [x] 1.4 Support unsigned mode (empty/absent `chunk-signature=`) and trailer block consumption.
- [x] 1.5 Unit tests (pure, table-driven): AWS documentation vector (`'a'` × 66560, three chunks, expected signatures), corrupted chunk signature, truncated/malformed chunk, missing terminating chunk, unsigned trailer variant, decoded-length accounting.

## 2. Phase 2: Signed chunk verification
- [x] 2.1 In `src/transport/sigv4.go`, export a helper that derives the streaming verification context from the verified request: seed signature (request signature), `deriveSigningKey`, `amzDate`, and credential scope.
- [x] 2.2 Implement chunk string-to-sign `AWS4-HMAC-SHA256-PAYLOAD\n<amzDate>\n<scope>\n<prevSig>\n<emptyStringSHA256>\n<hex(sha256(chunkData))>` and `hex(HMAC-SHA256(signingKey, stringToSign))` chaining.
- [x] 2.3 Wire verification into the de-framer's stream for `STREAMING-AWS4-HMAC-SHA256-PAYLOAD`; keep the canonical (header) signature check using the `STREAMING-*` literal unchanged.
- [x] 2.4 Unit tests proving the AWS documentation seed/chunk signatures reproduce exactly, and that tampered/reordered chunks fail.

## 3. Phase 3: Gateway integration (`s3_communicator.go`)
- [x] 3.1 Detect streaming PUTs in the `HandshakeServer` PUT path via `Content-Encoding: aws-chunked` and/or `STREAMING-*` `X-Amz-Content-Sha256`.
- [x] 3.2 Handle `Expect: 100-continue` (emit `HTTP/1.1 100 Continue\r\n\r\n`) before reading the body.
- [x] 3.3 Set a size-based read deadline around the body read (mirroring server `absoluteDeadline`); clear afterwards.
- [x] 3.4 Decode the body: verify chunk signatures (signed mode), spill de-framed bytes to a bounded temp file, compute decoded SHA-256/size.
- [x] 3.5 Resolve metadata: `m.meta.Hash = <decoded sha256>` and `m.meta.Size = <decoded size>`; cross-check `X-Amz-Decoded-Content-Length` (reject on mismatch); enforce `MaxFileSize`.
- [x] 3.6 Serve the de-framed payload from the spill via `S3Communicator.Read()`; close/remove the spill in `Close()` and on all error paths.
- [x] 3.7 Reject `STREAMING-*` on the OPTIONS-handshake PUT path in `ReceiveMetadata` (`400 InvalidArgument`): that path cannot authenticate chunk signatures.
- [x] 3.8 Error mapping: chunk sig mismatch → `403 SignatureDoesNotMatch`; malformed framing / decoded-length mismatch → `400 InvalidArgument`; oversized → `413 EntityTooLarge`.
- [x] 3.9 Transport-level tests: streaming PUT handshake stores de-framed content; dedup uses real hash; `Expect: 100-continue`; decoded-length mismatch; oversized; `STREAMING-*` via OPTIONS path rejected.

## 4. Phase 4: End-to-end and replication verification
- [x] 4.1 End-to-end test: streaming PUT → GET returns original bytes.
- [ ] 4.2 Splay and chain replication with a streaming upload: replicated content matches the original decoded bytes.
- [ ] 4.3 Verify dedup: two streaming uploads of identical content share one blob; hashes match `common.HashBytes`.
- [ ] 4.4 Benchmark the de-framer (large payload) for the performance hook; record before/after ns/op and allocations.

## 5. Docs
- [x] 5.1 Update `docs/PROTOCOL.md` (S3 section): streaming variants supported, de-framing at gateway boundary, chunk signature algorithm, unsigned-streaming security posture.
- [x] 5.2 Add the outbound `S3BlobStore` `UNSIGNED-PAYLOAD` justification note.

## 6. Validation
- [x] 6.1 Run `go vet`, `gofmt`, and the full per-module test suites (`src/root`, `common`, `transport`, `client`, `server`, `storage`, `metrics`, `crypto`, `p2p`).
- [x] 6.2 Run the performance hook and commit the `docs/PERFORMANCE.md` / `.github/data/benchmark_history.csv` updates (pre-commit hook syncs these).
- [ ] 6.3 Open PR with `Resolves #773`, wait for checks, address the `github-actions` review, merge `--merge --delete-branch`, close the issue, run the Rule 71 master gate.
