# UploadPartCopy

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1365

## Why

`UploadPartCopy` (`PUT /{bucket}/{key}?uploadId=…&partNumber=N` with
`X-Amz-Copy-Source`) was answered with `501 NotImplemented`. SDKs and `aws s3 cp`
use it to copy objects larger than the single-part limit server-side, and it is the
only way to assemble a multipart object from data already in the cluster without
moving the bytes through the client.

## What Changes

- The request is routed to a real handler instead of the honest-501 reject.
- The copy source is resolved by the same code `CopyObject` uses, so the two
  cannot drift on validation, bucket checks or error shapes.
- `x-amz-copy-source-range: bytes=start-end` is supported (inclusive on both ends,
  as S3 defines it), with S3's 5-byte minimum for a copied part range. An
  unsatisfiable, inverted, open-ended or malformed range answers `416 InvalidRange`
  / `400 InvalidArgument` rather than storing the wrong bytes.
- The source streams straight into the part spill (never into memory), hashing as
  it goes, so a copy costs disk, not heap.
- The response is a `<CopyPartResult>` with the part ETag and `LastModified`.

## Impact

- Affected specs: `s3-upload-part-copy`
- Affected code: `src/transport/s3_communicator.go`
- Removed from the unsupported-operation set; the compatibility check can exercise
  it with a real client.
