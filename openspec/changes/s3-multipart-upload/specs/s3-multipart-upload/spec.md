> GitHub Issue URL: https://github.com/alsotoes/momo/issues/764

# S3 Multipart Upload Support Specification

## Purpose
This specification adds S3 multipart upload endpoints to momo's S3 gateway, enabling AWS SDK clients (aws-cli, boto3, aws-sdk-go-v2) to upload objects above their multipart threshold without failures.

## ADDED Requirements

### Requirement: Multipart upload endpoint interception
The gateway SHALL intercept all six multipart REST endpoints in `HandshakeServer` and handle them end-to-end (returning `ErrRequestHandled`). Upload state SHALL be tracked in memory.

#### Scenario: CreateMultipartUpload
- **GIVEN** a `POST /{bucket}/{key}?uploads` request with valid auth
- **WHEN** the gateway processes it
- **THEN** it returns `200 OK` with an `InitiateMultipartUploadResult` XML body containing a unique `UploadId`

#### Scenario: UploadPart
- **GIVEN** a `PUT /{bucket}/{key}?uploadId=X&partNumber=N` request with valid auth
- **WHEN** the gateway processes it
- **THEN** it stores the part body in memory keyed by upload ID, returns `200 OK` with an `ETag` header (SHA-256 of part body)

#### Scenario: CompleteMultipartUpload
- **GIVEN** a `POST /{bucket}/{key}?uploadId=X` request with valid auth and all parts uploaded
- **WHEN** the gateway processes it
- **THEN** it sorts parts by part number, assembles them in order, computes SHA-256 of the assembled content, calls `store.Put()` with the assembled content, and returns `200 OK` with a `CompleteMultipartUploadResult` XML body

#### Scenario: AbortMultipartUpload
- **GIVEN** a `DELETE /{bucket}/{key}?uploadId=X` request with valid auth
- **WHEN** the gateway processes it
- **THEN** it cleans up the tracked part data and returns `204 No Content`

#### Scenario: ListParts
- **GIVEN** a `GET /{bucket}/{key}?uploadId=X` request with valid auth
- **WHEN** the gateway processes it
- **THEN** it returns `200 OK` with a `ListPartsResult` XML body containing per-part metadata

#### Scenario: ListMultipartUploads
- **GIVEN** a `GET /?uploads` or `GET /{bucket}?uploads` request with valid auth
- **WHEN** the gateway processes it
- **THEN** it returns `200 OK` with a `ListMultipartUploadsResult` XML body containing active uploads

### Requirement: In-memory state tracking
The gateway SHALL track multipart upload state using a `map[string]*multipartUpload` protected by `sync.Mutex`. Each upload SHALL store the bucket, key, creation time, and an ordered list of parts (part number, ETag, body data). On `CompleteMultipartUpload`, the upload SHALL be removed from the map after assembly.

### Requirement: Store integration
On `CompleteMultipartUpload`, the assembled object SHALL be stored through the standard `store.Put()` path so the object flows through the normal momo CAS/persistence/replication pipeline.

