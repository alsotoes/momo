# Bolt: Pre-Canonicalized Checksum Header Lookups

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1275

## Why

The S3 gateway probes four checksum headers on every request to discover whether a
client supplied an integrity checksum. The probe built each header name at
runtime (`"x-amz-checksum-" + algo`) and passed it to `http.Header.Get`, which
canonicalizes its argument. That allocated a string and re-ran canonicalization
four times per request, even when the request carried no checksum at all —
GC pressure on the hottest ingest path for no benefit.

## What Changes

- Replace the per-request dynamic header construction in `parseChecksum` and
  `collectS3Headers` with one package-level table of normalized algorithm plus
  pre-canonicalized header literal.
- Keep the probe order (CRC32C, CRC32, SHA1, SHA256) so algorithm precedence is
  unchanged.
- Add benchmarks that keep the saving measurable.

## Impact

- Affected specs: `bolt-checksum-headers`
- Affected code: `src/transport/s3_communicator.go`
- Behavioural change: none — same algorithms, same precedence, same errors.
