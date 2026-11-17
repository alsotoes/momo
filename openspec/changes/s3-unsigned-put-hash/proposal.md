# S3 Unsigned PUT Hash Derivation

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1346

## Why

A plain S3 PUT that carries no content hash was rejected before its body was
read: `S3Communicator` derived the object hash only from
`X-Amz-Content-Sha256` / `Content-Sha256`, so an unsigned upload (`curl -T`,
`UNSIGNED-PAYLOAD`, any client that does not sign the payload) arrived with an
empty hash, the daemon treated an empty hash as invalid, `success` stayed false,
the deferred ACK never ran and the socket closed mid-body. The client saw a
connection reset instead of an S3 error.

Because the SHA-256 is the CAS blob key, the hash must exist before ingest - but
it does not have to come from the client: the ingest path already computes and
verifies it while streaming.

## What Changes

- Derive the content hash for an external client's unsigned PUT: spool the body
  disk-first, hash it while copying, rewind, and replay it through the normal
  ingest path so the stored digest is still verified.
- Stop sending interim momo framing (`X-Momo-Metadata-Status`,
  `X-Momo-Replication-Mode`) and the momo ACK frame to external S3 clients: they
  now receive exactly one response, a real S3 `200 OK` with the content `ETag`.
- Refuse over-limit and short bodies with proper S3 errors (`413
  EntityTooLarge`, `400 IncompleteBody`) instead of closing the connection.

## Impact

- Affected specs: `s3-unsigned-put`
- Affected code: `src/transport/s3_communicator.go`
- Wire compatibility: unchanged for momo-framed clients and for peers; only the
  external-client response shape becomes standard S3.
