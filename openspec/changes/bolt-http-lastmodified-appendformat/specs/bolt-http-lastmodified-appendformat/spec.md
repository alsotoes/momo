> GitHub Issue URL: https://github.com/alsotoes/momo/issues/977

# bolt-http-lastmodified-appendformat Specification

## Purpose

Remove per-response heap allocations from the S3 GET/HEAD/304 HTTP header path
by using the standard-library `time.AppendFormat` idiom, keeping the emitted
`Last-Modified` header bytes byte-for-byte identical.

## ADDED Requirements

### Requirement: allocation-free Last-Modified header rendering

`S3Communicator.HandshakeServer` SHALL render the `Last-Modified` response
header using `time.AppendFormat` directly into the growing response byte slice
(`b = time.Unix(0, meta.ModTime).UTC().AppendFormat(b, http.TimeFormat)`),
without calling `time.Format()` and without any dynamic string allocation for
the timestamp.

#### Scenario: GET object success response
- **GIVEN** a completed S3 GET object operation with modification time
  `meta.ModTime`
- **WHEN** `HandshakeServer` builds the `200 OK` response headers
- **THEN** the `Last-Modified` header contains the UTC timestamp rendered as
  an HTTP IMF-fixdate (`http.TimeFormat`) and no heap string is allocated for
  it

#### Scenario: GET range / HEAD / 304 responses
- **GIVEN** an S3 GET range, HEAD, or 304 Not Modified response with
  modification time `meta.ModTime`
- **WHEN** `HandshakeServer` builds the response headers
- **THEN** the `Last-Modified` header uses the same allocation-free
  `AppendFormat` rendering and is byte-for-byte identical to the previous
  `formatHTTPLastModified` output

### Requirement: dead helper removal

The `formatHTTPLastModified` helper SHALL be removed once all call sites use
`time.AppendFormat`.

#### Scenario: no dangling references
- **GIVEN** the removal of `formatHTTPLastModified`
- **WHEN** the transport package is built
- **THEN** no call site references the removed helper and the build succeeds