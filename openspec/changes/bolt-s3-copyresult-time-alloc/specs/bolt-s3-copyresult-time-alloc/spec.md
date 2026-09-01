> GitHub Issue URL: https://github.com/alsotoes/momo/issues/968

# bolt-s3-copyresult-time-alloc Specification

## Purpose

Remove a per-response heap allocation from the S3 CopyObject success path by
using the standard-library `time.AppendFormat` idiom with a stack-allocated
scratch buffer, keeping the emitted XML byte-for-byte identical.

## ADDED Requirements

### Requirement: allocation-free LastModified formatting

`FormatCopyObjectResultXML` SHALL render the `LastModified` element using
`time.AppendFormat` with a stack-allocated `[32]byte` scratch buffer written
directly into the response `bytes.Buffer`, without calling `time.Format()` and
without any dynamic string allocation for the timestamp.

#### Scenario: CopyObject success response
- **GIVEN** a completed S3 CopyObject operation with modification time `modTime`
- **WHEN** `FormatCopyObjectResultXML` builds the result XML
- **THEN** the `LastModified` element contains the UTC timestamp rendered as
  `2006-01-02T15:04:05.000Z` and no heap string is allocated for it

#### Scenario: timestamp layout unchanged
- **GIVEN** the pre-change `formatLastModified` output
- **WHEN** the same instant is formatted by the new `AppendFormat` path
- **THEN** the emitted byte sequence is identical (RFC3339-style layout with
  millisecond precision and an uppercase `Z`)

## UNCHANGED Behavior
- `formatLastModified` remains in use at its other call sites.
- XML structure, escaping, and ETag handling in the CopyObject result are
  unchanged.
- CopyObject request semantics and S3 response codes are unchanged.