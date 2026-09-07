## 1. Implementation
- [x] 1.1 Implement the `S3Communicator` struct in `src/common`.
- [x] 1.2 Implement S3 Header parsing for `Content-SHA256` and `Content-Length`.
- [x] 1.3 Add a basic REST listener in `src/server` to handle `PUT` and `GET` verbs.
- [x] 1.4 Integrate the S3 handler into the `ProtocolFactory`.
- [x] 1.5 Map S3 authentication to the existing Momo `AuthToken` standard.

## 2. Verification
- [x] 2.1 Unit tests for S3 metadata mapping.
- [x] 2.2 Integration tests using `aws-cli` or `s3cmd` against a local Momo node.
- [x] 2.3 Assert that `Chain` replication works correctly when triggered by an S3 upload.
