# 0111-s3-upload-part-copy

## Status
Proposed

## Confidence
Low

## Context
`UploadPartCopy` (`PUT /{bucket}/{key}?uploadId=…&partNumber=N` with
`X-Amz-Copy-Source`) was answered with `501 NotImplemented`. SDKs and `aws s3 cp`
use it to copy objects larger than the single-part limit server-side, and it is the
only way to assemble a multipart object from data already in the cluster without
moving the bytes through the client.

## Decision
- Copy A Source Object Into A Part: `PUT ?uploadId&partNumber` with `X-Amz-Copy-Source` SHALL store the source object's bytes as a part of the open upload.
- Ranged Copy: `x-amz-copy-source-range: bytes=start-end` SHALL store exactly that slice.
- Honest Failures: 
- Bounded Memory: 

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1365
- PR: 
- Spec: `openspec/changes/s3-upload-part-copy/`
- Blog: 

