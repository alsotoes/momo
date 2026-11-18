# UploadPartCopy

## Purpose

Let a client build a multipart object from bytes already held by the cluster.

## Requirements

### Requirement: Copy A Source Object Into A Part

`PUT ?uploadId&partNumber` with `X-Amz-Copy-Source` SHALL store the source object's
bytes as a part of the open upload.

#### Scenario: Whole object

- **WHEN** a client copies a source object into one part and completes the upload
- **THEN** the assembled object equals the source bytes

#### Scenario: Response

- **WHEN** the copy succeeds
- **THEN** the response is a `<CopyPartResult>` carrying the part ETag and
  `LastModified`

### Requirement: Ranged Copy

`x-amz-copy-source-range: bytes=start-end` SHALL store exactly that slice.

#### Scenario: Valid range

- **WHEN** the client copies bytes 10-14 of a 20-byte object
- **THEN** the part holds exactly those five bytes

#### Scenario: Unsatisfiable range

- **WHEN** the range is inverted, beyond the object, open-ended, malformed, or
  shorter than five bytes
- **THEN** the request is refused with `416 InvalidRange` / `400 InvalidArgument`
  and no part is stored

### Requirement: Honest Failures

#### Scenario: Missing source

- **WHEN** the source object does not exist
- **THEN** the request is refused with `404 NoSuchKey`, including when the backend
  wraps the not-found error

#### Scenario: Unknown upload

- **WHEN** the upload id does not exist (or belongs to another bucket or key)
- **THEN** the request is refused with `404 NoSuchUpload`

### Requirement: Bounded Memory

#### Scenario: Large copy

- **WHEN** the copied part is several GiB
- **THEN** the daemon's heap does not grow with the part size, because the source
  streams into the part spill
