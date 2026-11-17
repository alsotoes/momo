# Composite Checksum Omission

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1354

## Why

A multipart object's checksum is a **composite**: a checksum of the part
checksums, written by S3 with a `-<partCount>` suffix. It can never validate
against the whole object. Answering with it — even in checksum mode — makes a
standard client abort the download with
`AWS_ERROR_S3_RESPONSE_CHECKSUM_MISMATCH`, which the S3 client compatibility check
(#1350) reproduces on every run.

## What Changes

- Detect a composite value by its `-<partCount>` suffix.
- Never echo a composite checksum on GET/HEAD.
- In checksum mode, compute a checksum over the stored bytes when the persisted
  value is composite (the same path already used for objects without one), so the
  client validates a value that actually describes the bytes it receives.

## Impact

- Affected specs: `s3-composite-checksum`
- Affected code: `src/transport/s3_communicator.go`
- Response headers become trustworthy: a returned checksum always describes the
  served object.
