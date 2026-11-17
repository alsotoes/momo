# 0107-s3-composite-checksum-omission

## Status
Proposed

## Confidence
Low

## Context
A multipart object's checksum is a **composite**: a checksum of the part
checksums, written by S3 with a `-<partCount>` suffix. It can never validate
against the whole object. Answering with it — even in checksum mode — makes a
standard client abort the download with
`AWS_ERROR_S3_RESPONSE_CHECKSUM_MISMATCH`, which the S3 client compatibility check
(#1350) reproduces on every run.

## Decision
- Composite Checksums Are Not Returned: A GET or HEAD response SHALL NOT carry a composite (multipart) checksum, which is recognisable by its `-<partCount>` suffix.
- Whole-Object Checksums Still Returned: A checksum that describes the whole object SHALL still be returned when the client asks for it.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1354
- PR: 
- Spec: `openspec/changes/s3-composite-checksum-omission/`
- Blog: 

