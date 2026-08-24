> GitHub Issue URL: https://github.com/alsotoes/momo/issues/920

# s3-p5-remaining-subresource-501 Specification

## Purpose
Return a clean S3-compliant `501 NotImplemented` for the remaining unsupported
S3 operations that still silently misroute after the P3/P4 subresource reject
work: SelectObjectContent (`?select`), UploadPartCopy
(`PUT ?uploadId&partNumber` plus `X-Amz-Copy-Source`), and the bucket analytics /
inventory / metrics / intelligent-tiering configuration subresources. Only the
honest reject is in scope; no feature is implemented.

## ADDED Requirements

### Requirement: bucket analytics/inventory/metrics/intelligent-tiering rejected
The system SHALL reject `?analytics`, `?inventory`, `?metrics` and
`?intelligent-tiering` addressed at a bucket root (`key == ""`) with `501` code
`NotImplemented`, consistent with the existing bucket-config reject set.

#### Scenario: analytics configuration rejected
- **GIVEN** a `GET /bucket?analytics` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented`

#### Scenario: inventory configuration rejected
- **GIVEN** a `PUT /bucket?inventory` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented`

### Requirement: SelectObjectContent rejected
The system SHALL reject `?select` addressed at an object key with `501` code
`NotImplemented`.

#### Scenario: select object rejected
- **GIVEN** a `POST /bucket/key?select&select-type=2` request with valid auth
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented` and no bytes are read

### Requirement: UploadPartCopy rejected
The system SHALL reject a `PUT` carrying both multipart params (`?uploadId`,
`?partNumber`) and an `X-Amz-Copy-Source` header with `501` code `NotImplemented`,
instead of misreading the copy source as a part body.

#### Scenario: upload-part-copy rejected
- **GIVEN** a `PUT /bucket/key?uploadId=X&partNumber=1` request with valid auth
  and an `X-Amz-Copy-Source: /bucket/src` header
- **WHEN** processed by the gateway
- **THEN** the response is `501` with code `NotImplemented` and no part is stored

#### Scenario: plain upload-part still served
- **GIVEN** a `PUT /bucket/key?uploadId=X&partNumber=1` request with valid auth
  and no `X-Amz-Copy-Source` header
- **WHEN** processed by the gateway
- **THEN** the existing UploadPart behavior is unchanged
