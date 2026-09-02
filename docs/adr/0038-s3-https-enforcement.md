# 0038-s3-https-enforcement

## Status
Accepted

## Confidence
High

## Context
The S3 backend client (`S3BlobStore.S3Endpoint`) accepts any scheme (`http://` or `https://`) without validation, sending SigV4 credentials, blob content, and object metadata in cleartext when the endpoint is `http://`. AWS requires TLS for S3; the current highest practice is HTTPS-only plus bucket encryption at rest.

## Decision
- Endpoint scheme validation on startup: `NewS3BlobStore` SHALL validate the `s3_endpoint` URL scheme before returning. If the scheme is anything other than `https://` or `http://`, or if no scheme is present, the constructor SHALL return an `EINVAL` error.
- Prominent warning when insecure is enabled: When `s3_insecure = true` and the endpoint uses `http://`, the SHALL log a prominent warning message indicating that credentials and blob content are transmitted without TLS.

## Consequences


## Alternatives Considered
None documented.

## Confidence
High

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/s3-https-enforcement/
- Blog: docs/blog/posts/...md
