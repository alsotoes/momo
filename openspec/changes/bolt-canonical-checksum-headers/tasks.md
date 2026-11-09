# Tasks — Bolt: Pre-Canonicalized Checksum Header Lookups

- [x] Add `checksumRequestHeaders` table (algorithm + canonical literal) in
  `src/transport/s3_communicator.go`.
- [x] Use the table in `parseChecksum` (probe order preserved: CRC32C, CRC32,
  SHA1, SHA256).
- [x] Use the table in `collectS3Headers` so persistence still captures every
  algorithm and infers the marker.
- [x] Tests: table matches canonical form of the response header name, every
  algorithm is discovered, every algorithm is captured and marked.
- [x] Benchmarks: `BenchmarkParseChecksum` (0 B/op, 0 allocs/op) and
  `BenchmarkParseChecksumDynamicBaseline` (96 B/op, 4 allocs/op).
