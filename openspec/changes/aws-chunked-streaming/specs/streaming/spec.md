> GitHub Issue URL: https://github.com/alsotoes/momo/issues/773

# S3 aws-chunked Streaming Payload Specification

## Purpose
This specification adds support for S3 streaming uploads on the momo gateway:
AWS SDK clients that use `STREAMING-AWS4-HMAC-SHA256-PAYLOAD` (signed),
`STREAMING-UNSIGNED-PAYLOAD-TRAILER` (unsigned), or `aws-chunked` framing under
`UNSIGNED-PAYLOAD`. The gateway decodes the `aws-chunked` frames at the
transport boundary — before content enters the momo pipeline — so stored blobs
are the de-framed content (matching momo content-addressing and dedup), the
content hash used by placement/replication is the real decoded SHA-256, and
signed chunk streams are verified per-chunk. The standard PUT/replication path
(the server, storage, and momo wire protocol) is preserved unchanged.

## ADDED Requirements

### Requirement: Streaming Upload Detection
The system SHALL identify an S3 upload as a streaming (aws-chunked) upload when
the PUT request's `Content-Encoding` header contains `aws-chunked` OR its
`X-Amz-Content-Sha256` header is `STREAMING-AWS4-HMAC-SHA256-PAYLOAD`,
`STREAMING-AWS4-HMAC-SHA256-PAYLOAD-TRAILER`,
`STREAMING-UNSIGNED-PAYLOAD-TRAILER`, or any other `STREAMING-*` literal.

#### Scenario: Signed streaming upload via AWS SDK
- **WHEN** an S3 SDK sends a PUT with `X-Amz-Content-Sha256: STREAMING-AWS4-HMAC-SHA256-PAYLOAD` and `Content-Encoding: aws-chunked`
- **THEN** the gateway detects the streaming body and decodes it instead of treating the frames as object content

#### Scenario: Unsigned streaming with checksum trailer
- **WHEN** a client sends a PUT with `X-Amz-Content-Sha256: STREAMING-UNSIGNED-PAYLOAD-TRAILER` and `Content-Encoding: aws-chunked`
- **THEN** the gateway detects the streaming body, decodes the chunks, and consumes the trailing headers without storing them

#### Scenario: Non-streaming upload unaffected
- **WHEN** a client sends a PUT whose `X-Amz-Content-Sha256` is a plain 64-hex hash and `Content-Encoding` does not contain `aws-chunked`
- **THEN** the gateway treats the body as raw content exactly as today

### Requirement: aws-chunked De-framing
The system SHALL decode the `aws-chunked` body into its raw content. Each chunk
on the wire is `<hex-size>[;chunk-signature=<64-hex>]\r\n` followed by the
raw (binary) chunk data bytes, followed by `\r\n`. The stream ends with a
`0`-size chunk (`0;chunk-signature=<sig>\r\n\r\n`), optionally followed by a
trailer block (`<name>:<value>\r\n` lines terminated by a blank line) for the
`*-TRAILER` variants. The chunk signature extension may be absent in unsigned
mode. De-framing SHALL stream (bounded memory, disk spill, fixed-size internal
buffers) and SHALL compute the decoded SHA-256 and decoded byte count.

#### Scenario: Streaming PUT round-trips raw content
- **GIVEN** an S3 SDK uploads `N` bytes as an aws-chunked body
- **WHEN** the gateway decodes the frames and stores the object
- **THEN** the stored blob equals exactly the original `N` bytes (no sizes, signatures, CRLFs, or trailers)

#### Scenario: Bounded memory for large streaming uploads
- **WHEN** an very large streaming PUT is received
- **THEN** the gateway decodes using a disk-backed spill and fixed buffers, never buffering the whole decoded payload in memory

#### Scenario: Malformed framing rejected
- **WHEN** a chunk header has a non-hex size, a truncated chunk, a chunk whose data is shorter than the declared size, or a missing terminating `0` chunk
- **THEN** the gateway rejects the upload with `400 InvalidArgument` and cleans up the spill

### Requirement: Content Hash and Size Resolution
For a streaming upload, the system SHALL NOT use the `STREAMING-*` literal or
the framed `Content-Length` as the momo payload metadata. It SHALL set
`meta.Hash` to the SHA-256 hex of the decoded content and `meta.Size` to the
decoded byte count. When `X-Amz-Decoded-Content-Length` is present it SHALL be
cross-checked against the decoded byte count and a mismatch SHALL reject the
upload. The de-framed content is then readable through `S3Communicator.Read()`
so dedup (`store.Has`), CRUSH placement, `getFile` hash verification, and
splay/chain replication use the real content hash and pass unchanged.

#### Scenario: Dedup hit on streaming re-upload
- **GIVEN** the same decoded content is uploaded twice via separate streaming PUTs
- **WHEN** the second upload completes
- **THEN** both names map to the same decoded SHA-256 blob and the store dedupes (blob written once)

#### Scenario: `getFile` integrity check passes
- **WHEN** a streaming PUT finishes decoding and the server stores it via the standard pipeline
- **THEN** the server's computed SHA-256 over the de-framed bytes equals `meta.Hash` (EBADMSG mismatch path never triggers)

#### Scenario: Decoded-length mismatch rejected
- **WHEN** the decoded byte count differs from the declared `X-Amz-Decoded-Content-Length`
- **THEN** the gateway rejects the upload with `400 InvalidArgument`

### Requirement: Signed Chunk Verification (STREAMING-AWS4-HMAC-SHA256-PAYLOAD)
For signed streaming, the system SHALL verify every chunk against the chained
SigV4 algorithm, using the request signature as the seed. For each chunk, the
signing key is `deriveSigningKey(secretKey, dateStamp, region)` and the chunk
string-to-sign is:
`"AWS4-HMAC-SHA256-PAYLOAD\n" + <amzDate> + "\n" + <dateStamp>/<region>/s3/aws4_request + "\n" + <previousChunkSignature> + "\n" + <emptyStringSHA256> + "\n" + <sha256hex(chunkData)>`
where `<emptyStringSHA256>` is the SHA-256 of the empty string
(`e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855`).
The expected signature is `hex(HMAC-SHA256(signingKey, stringToSign))`; it
becomes the `<previousChunkSignature>` of the next chunk. A mismatch SHALL
reject the upload with `403 SignatureDoesNotMatch`.

#### Scenario: AWS documentation test vector accepted
- **GIVEN** the AWS SigV4 streaming documentation example (access key `AKIAIOSFODNN7EXAMPLE`, secret `wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY`, region `us-east-1`, date `20130524T000000Z`, `66560` bytes of `'a'`)
- **WHEN** the gateway verifies the request and chunk signatures
- **THEN** the header signature matches `4f232c4386841ef735655705268965c44a0e4690baa4adea153f7db9fa80a0a9`, chunk 1 `ad80c730a21e5b8d04586a2213dd63b9a0e99e0e2307b0ade35a65485a288648`, chunk 2 `0055627c9e194cb4542bae2aa5492e3c1575bbb81b612b7d234b86a503ef5497`, and the terminating chunk `b6c6ea8a5354eaf15b3cb7646744f4275b71ea724fed81ceb9323e279d449df9`

#### Scenario: Tampered chunk rejected
- **WHEN** a chunk's bytes are altered after signing (or a chunk is reordered)
- **THEN** the gateway rejects the upload with `403 SignatureDoesNotMatch`

#### Scenario: Header signature uses the STREAMING literal
- **WHEN** a signed streaming PUT is received
- **THEN** `verifySigV4Signature` validates the canonical request using the `STREAMING-AWS4-HMAC-SHA256-PAYLOAD` literal as the payload hash (per AWS spec), and a regression test locks this behavior in

### Requirement: Unsigned Streaming (STREAMING-UNSIGNED-PAYLOAD-TRAILER and aws-chunked under UNSIGNED-PAYLOAD)
The system SHALL accept unsigned streaming bodies, de-frame them, and skip
per-chunk signature verification when the chunk signature field is empty or
absent. The security posture SHALL be documented: without per-chunk signatures
the stream itself is unauthenticated between the header signature check and
storage; integrity is still provided by the content-addressing hash computed
during decode (a wrong/hand-edited body simply produces a different content
hash, which is stored under its own key — it cannot corrupt another object).

#### Scenario: Unsigned trailer upload decodes cleanly
- **WHEN** a client sends `STREAMING-UNSIGNED-PAYLOAD-TRAILER` with empty chunk signatures and checksum trailing headers
- **THEN** the gateway stores the de-framed content and ignores/consumes the trailers without error

### Requirement: DoS Bounds, Deadlines, and 100-continue
The system SHALL set a read deadline sized to the decoded content length
(bounded, mirroring the server's size-based deadline) around the streaming body
read and clear it afterwards. If the PUT carries an `Expect: 100-continue`
header, the gateway SHALL send `HTTP/1.1 100 Continue\r\n\r\n` before reading
the body. Chunk header lines SHALL be length-bounded, individual chunk size
SHALL be capped (AWS maximum), and the total decoded size SHALL be capped at
`common.MaxFileSize`.

#### Scenario: Client that waits for 100-continue
- **WHEN** a streaming PUT includes `Expect: 100-continue`
- **THEN** the gateway responds `100 Continue` before reading the body, and the upload proceeds

#### Scenario: Oversized streaming upload rejected
- **WHEN** the decoded size would exceed `common.MaxFileSize`
- **THEN** the gateway rejects the upload with `413 EntityTooLarge` before storing anything

#### Scenario: Stalled streaming upload fails the deadline
- **WHEN** a streaming body stops arriving past the size-based read deadline
- **THEN** the connection read fails and the upload aborts rather than hanging indefinitely

### Requirement: Spill Cleanup
The system SHALL clean up the decoded-content spill file (and any partial
blob) on every exit path: success, signature failure, framing error,
size overflow, and connection error.

#### Scenario: Failure mid-stream leaves no residue
- **WHEN** a streaming upload fails after partial decoding (bad chunk signature, malformed frame, or aborted connection)
- **THEN** no spill file and no stored object remain for that request

### Requirement: Regression Tests
The system SHALL ship regression tests covering: chunk de-framing parser
(valid signed stream, corrupted signature, truncated/failed chunk, unsigned
trailer variant); SigV4 canonical-signature verification with the
`STREAMING-*` literal; the full end-to-end streaming PUT → GET round trip;
decoded-length mismatch rejection; `Expect: 100-continue`; oversized upload
rejection; and streaming PUT through splay/chain replication producing
identical replicated content.

#### Scenario: End-to-end streaming PUT/GET round trip
- **WHEN** an S3 client uploads an object with a signed streaming body and then GETs it
- **THEN** the object content equals the original pre-framing bytes

### Requirement: Documentation
The system SHALL document in `docs/PROTOCOL.md`: the supported `aws-chunked`
variants, the de-framing behavior at the gateway boundary, the chunk signature
verification algorithm, the unsigned-streaming security posture, and a note
explaining why the outbound `S3BlobStore` may continue to use
`UNSIGNED-PAYLOAD` (its blob keys are momo content hashes and it is an
opaque backend, so per-chunk upload signatures add no integrity value).