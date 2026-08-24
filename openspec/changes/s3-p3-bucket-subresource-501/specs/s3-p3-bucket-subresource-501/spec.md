> GitHub Issue URL: https://github.com/alsotoes/momo/issues/912

# s3-p3-bucket-subresource-501 Specification

## Purpose
Return a clean S3-compliant `501 NotImplemented` for unsupported bucket-config
subresource query params addressed at the bucket root, instead of letting them
fall through to the nearest method handler (which misroutes them into
ListObjectsV2/CreateBucket/GetObject). This is Tier P3 of #820. Only the honest
reject is in scope; no bucket-config feature is implemented.

## ADDED Requirements

### Requirement: clean rejection of unsupported bucket-config subresources
The system SHALL intercept a defined set of unsupported bucket-config
subresource query params when addressed at a bucket root (`key == ""`) for any
HTTP method, and SHALL respond `501` with the S3 `NotImplemented` error code
without invoking store operations.

#### Scenario: versioning get rejected
- **GIVEN** an S3 `GET /bucket?versioning` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented` and no object is read

#### Scenario: tagging put rejected
- **GIVEN** an S3 `PUT /bucket?tagging` request with valid auth and bucket mode
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented` and no bucket is created

#### Scenario: cors delete rejected
- **GIVEN** an S3 `DELETE /bucket?cors` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented`

### Requirement: supported subresources unaffected
The system SHALL continue to route supported subresources exactly as before.

#### Scenario: location and list still served
- **GIVEN** an S3 `GET /bucket?location` or a list request (`?list-type=2`,
  pagination params)
- **WHEN** processed by the gateway
- **THEN** the existing `200` responses (GetBucketLocation / ListObjectsV2) are returned unchanged

#### Scenario: multipart and batch delete still served
- **GIVEN** multipart (`?uploads`, `?uploadId`, `?partNumber`) or
  `POST /bucket?delete` (DeleteObjects) requests
- **WHEN** processed by the gateway
- **THEN** the existing handler behavior is unchanged

### Requirement: object-level subresources remain out of scope
The system SHALL NOT change routing for object-targeted subresources
(e.g. `GET /bucket/key?tagging`, `?versionId`), which belong to Tier P4.

#### Scenario: object tagging untouched
- **GIVEN** an S3 `GET /bucket/key?tagging` request with valid auth
- **WHEN** processed by the gateway
- **THEN** routing behavior is unchanged from before this change
