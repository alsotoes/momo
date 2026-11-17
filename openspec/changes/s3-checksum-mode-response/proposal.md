# S3 Checksum-Mode Response Headers

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1352

## Why

`appendS3MetaHeaders` echoed every persisted S3 header, including
`x-amz-checksum-*`, on every GET and HEAD. Checksum headers belong to *checksum
mode*: S3 returns them only when the request asks with
`x-amz-checksum-mode: ENABLED`.

Echoing them unsolicited makes a client validate a value it never requested. For
a multipart object the stored value is the **composite** checksum (checksum of
the part checksums), which by definition differs from the checksum of the
downloaded bytes, so a modern aws-cli (2.23+, response checksum validation on by
default) aborts the download with `AWS_ERROR_S3_RESPONSE_CHECKSUM_MISMATCH`.

## What Changes

- Emit `x-amz-checksum-*` only when the request sets
  `x-amz-checksum-mode: ENABLED`, on GET and HEAD.
- Leave the explicit checksum-mode path untouched: it still returns the stored
  checksum, or computes one for objects that never had one.

## Impact

- Affected specs: `s3-checksum-mode`
- Affected code: `src/transport/s3_communicator.go`
- No wire-format change; response headers become spec-conformant.
