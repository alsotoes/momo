# Verify aws-chunked Trailing Checksums

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1363

## Why

A client streaming an upload with aws-chunked can assert integrity in a header or
in a **trailer** at the end of the body. The gateway accepted the trailer form but
discarded the value, so a corrupted stream was stored silently: the object is
keyed by the SHA-256 of the bytes received, and nothing compared those bytes with
what the client meant to send.

`COMPATIBILITY.md` recorded this as a known ceiling.

## What Changes

- The aws-chunked decoder captures the trailing-header block instead of discarding
  it, bounded three ways: 64 KiB total, 16 entries, and a strict header-name
  character set so a trailer cannot smuggle bytes into a log line or a response.
- After decoding, a trailing `x-amz-checksum-<algo>` is verified over the decoded
  bytes before anything is stored. Mismatch → `400 BadDigest`; unsupported
  algorithm → `400 InvalidRequest`; malformed trailer → `400 InvalidArgument`.
- A trailer declared in `X-Amz-Trailer` but never delivered is treated as "no
  checksum": there is nothing to verify, and refusing it would reject uploads S3
  would not validate either.
- Multipart parts get the same protection, because a part body goes through the
  same decoder.

## Impact

- Affected specs: `s3-trailer-checksum`
- Affected code: `src/transport/aws_chunked.go`, `src/transport/s3_communicator.go`
- Integrity: a corrupt streamed upload is now refused instead of stored.
