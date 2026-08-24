> GitHub Issue URL: https://github.com/alsotoes/momo/issues/914

# s3-p4-object-subresource-501 Specification

## Purpose
Return a clean S3-compliant `501 NotImplemented` for unsupported object-level
subresource query params addressed at an object key, instead of letting them
fall through to the nearest method handler (which silently misroutes `?tagging`,
`?acl`, `?versionId`, `?retention`, `?legal-hold` into GetObject/PutObject/
DeleteObject). This is Tier P4 of #820. Only the honest reject is in scope; no
object ACL/tagging/versioning/retention feature is implemented.

## ADDED Requirements

### Requirement: clean rejection of unsupported object subresources
The system SHALL intercept a defined set of unsupported object subresource
query params when addressed at an object key (`key != ""`) for any HTTP method,
and SHALL respond `501` with the S3 `NotImplemented` error code without invoking
store operations.

#### Scenario: tagging get rejected
- **GIVEN** an S3 `GET /bucket/key?tagging` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented` and no object is read

#### Scenario: acl put rejected
- **GIVEN** an S3 `PUT /bucket/key?acl` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented` and no object is written

#### Scenario: versioned delete rejected
- **GIVEN** an S3 `DELETE /bucket/key?versionId=...` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented` and no object is deleted

### Requirement: supported object subresources unaffected
The system SHALL continue to route supported object params (`?uploadId`,
`?partNumber`) exactly as before.

#### Scenario: multipart still served
- **GIVEN** an S3 `PUT /bucket/key?uploadId=X&partNumber=N` (UploadPart) or
  `GET /bucket/key?uploadId=X` (ListParts) request with valid auth
- **WHEN** processed by the gateway
- **THEN** the existing handler behavior is unchanged

### Requirement: bucket-root subresources unaffected
The system SHALL retain the bucket-config rejection introduced in #912/#913.

#### Scenario: bucket-root versioning still rejected
- **GIVEN** an S3 `GET /bucket?versioning` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented`
