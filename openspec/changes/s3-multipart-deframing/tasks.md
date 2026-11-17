# Tasks

## 1. Gateway

- [x] 1.1 De-frame an aws-chunked part through the shared decoder
- [x] 1.2 Bound the part read by the request content length
- [x] 1.3 Spool parts to disk and adopt the decoder's spill (no second copy)
- [x] 1.4 Stream spooled parts into the store at completion
- [x] 1.5 Remove the spill on completion and abort
- [x] 1.6 Omit checksums on 206 responses

## 2. Decoder

- [x] 2.1 Accept any `io.Reader` (buffered fast path kept)
- [x] 2.2 Trailer block follows the terminating chunk with no extra CRLF

## 3. Verification

- [x] 3.1 Integration test: chunked part + plain part → decoded object, exact size
- [x] 3.2 Unit test: 206 carries no checksum header
- [x] 3.3 `aws-cli` 2.40 multipart round-trip in the S3 client compatibility check,
      with an explicit stored-size assertion
- [x] 3.4 `make test`, `make lint`, `gofmt`

## 4. Docs

- [x] 4.1 ARCHITECTURE: part de-framing, spill-backed parts, range checksum rule
- [x] 4.2 TESTING: multipart regression suite
