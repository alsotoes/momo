# 0105-s3-checksum-mode-response

## Status
Proposed

## Confidence
Low

## Context
`appendS3MetaHeaders` echoed every persisted S3 header, including
`x-amz-checksum-*`, on every GET and HEAD. Checksum headers belong to *checksum
mode*: S3 returns them only when the request asks with
`x-amz-checksum-mode: ENABLED`.

Echoing them unsolicited makes a client validate a value it never requested. For
a multipart object the stored value is the **composite** checksum (checksum of
the part checksums), which by definition differs from the checksum of the
downloaded bytes, so a modern aws-cli (2.23+, response checksum validation on by
default) aborts the download with `AWS_ERROR_S3_RESPONSE_CHECKSUM_MISMATCH`.

## Decision
- Checksums Only In Checksum Mode: A GET or HEAD response SHALL carry `x-amz-checksum-*` only when the request includes `x-amz-checksum-mode: ENABLED`.
- Other Metadata Is Unaffected: Preserved object metadata (Content-Type, `x-amz-meta-*`, cache/encoding headers) SHALL continue to be echoed on every response.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Planned
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1352
- PR: 
- Spec: `openspec/changes/s3-checksum-mode-response/`
- Blog: 

