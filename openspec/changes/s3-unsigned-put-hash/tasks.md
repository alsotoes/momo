# Tasks

## 1. Gateway

- [x] 1.1 Derive the SHA-256 for an external client's unsigned PUT (spool, hash,
      replay) in `bufferUnsignedPayload`
- [x] 1.2 Honour `Expect: 100-continue` before reading the body
- [x] 1.3 Refuse an over-limit declared length with `413 EntityTooLarge`
- [x] 1.4 Refuse a short body with `400 IncompleteBody`
- [x] 1.5 Suppress interim momo framing for external clients
      (`SendMetadataStatus`, `SendReplicationMode`)
- [x] 1.6 Answer an external client's completed upload with a single S3
      `200 OK` + `ETag`

## 2. Tests

- [x] 2.1 Unsigned PUT derives the content hash and stores the object
- [x] 2.2 The client sees exactly one S3 response with the ETag (no momo framing)
- [x] 2.3 Oversized declared length is refused with `413 EntityTooLarge`
- [x] 2.4 Signed/framed uploads are unchanged (transport, server, client suites)
- [x] 2.5 End-to-end: `test-e2e.sh s3-tcp`, `test-e2e.sh momo-tcp`,
      `test-e2e-encryption.sh s3-tcp`

## 3. Docs

- [x] 3.1 Document unsigned-PUT behaviour and the response shape
- [x] 3.2 Add the test suite to `TESTING.md`
