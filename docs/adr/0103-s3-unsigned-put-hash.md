# 0103-s3-unsigned-put-hash

## Status
Accepted

## Confidence
High

## Context
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

## Decision
- Derive The Hash For An Unsigned PUT: The gateway SHALL derive the object SHA-256 from the received bytes when an external S3 client supplies no `X-Amz-Content-Sha256` or `Content-Sha256` header.
- One Standard Response For External Clients: The gateway SHALL NOT send interim momo framing or the momo ACK frame to an external S3 client.
- Integrity Is Never Weakened: The gateway SHALL still refuse a supplied hash that does not match the stored bytes, and SHALL still reject hash values containing path-traversal characters.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1346
- PR: 
- Spec: `openspec/changes/s3-unsigned-put-hash/`
- Blog: 

