> GitHub Issue URL: https://github.com/alsotoes/momo/issues/774

# S3 — HTTPS Enforcement Specification

## Purpose
This specification requires TLS on the S3 storage backend endpoint to prevent silent cleartext transmission of SigV4 credentials, blob content, and object metadata when the endpoint is configured as `http://`.

## ADDED Requirements

### Requirement: Endpoint scheme validation on startup
`NewS3BlobStore` SHALL validate the `s3_endpoint` URL scheme before returning. If the scheme is anything other than `https://` or `http://`, or if no scheme is present, the constructor SHALL return an `EINVAL` error.

#### Scenario: https endpoint accepted
- **GIVEN** an `s3_endpoint` with the `https://` scheme
- **WHEN** `NewS3BlobStore` is called
- **THEN** the store is created successfully

#### Scenario: http endpoint rejected without insecure flag
- **GIVEN** an `s3_endpoint` with the `http://` scheme and `s3_insecure = false` (default)
- **WHEN** `NewS3BlobStore` is called
- **THEN** the constructor returns an `EINVAL` error

#### Scenario: http endpoint accepted with explicit insecure flag
- **GIVEN** an `s3_endpoint` with the `http://` scheme and `s3_insecure = true`
- **WHEN** `NewS3BlobStore` is called
- **THEN** the store is created and a prominent `WARNING` is logged

#### Scenario: missing scheme rejected
- **GIVEN** an `s3_endpoint` without a scheme (e.g., `s3.amazonaws.com`)
- **WHEN** `NewS3BlobStore` is called
- **THEN** the constructor returns an `EINVAL` error

#### Scenario: unsupported scheme rejected
- **GIVEN** an `s3_endpoint` with an unsupported scheme (e.g., `ftp://`)
- **WHEN** `NewS3BlobStore` is called
- **THEN** the constructor returns an `EINVAL` error

### Requirement: Prominent warning when insecure is enabled
When `s3_insecure = true` and the endpoint uses `http://`, the SHALL log a prominent warning message indicating that credentials and blob content are transmitted without TLS.

