# Tasks

- [x] 1.1 Capture the trailing-header block in the decoder (bounded, validated)
- [x] 1.2 Verify a trailing `x-amz-checksum-<algo>` over the decoded bytes
- [x] 1.3 `400 BadDigest` on mismatch, no object stored
- [x] 1.4 Reject unsupported algorithms and malformed/oversized trailers
- [x] 1.5 Treat a declared-but-absent trailer as "no checksum"
- [x] 1.6 Cover multipart parts (shared decoder)
- [x] 1.7 Tests: selection, name charset, matching accepted, mismatch refused
- [x] 1.8 Real client: `aws s3 cp` still round-trips with trailers
- [x] 1.9 Docs: COMPATIBILITY ceiling removed, ARCHITECTURE, TESTING
