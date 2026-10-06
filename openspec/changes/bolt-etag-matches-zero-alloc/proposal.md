# Change: Bolt — zero-allocation ETag header parsing

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1133 (tracking)

## Why

`etagMatches` parses a comma-separated entity-tag list from the `If-Match` /
`If-None-Match` request headers on the S3 conditional-request hot path. The
current implementation uses `strings.Split(header, ",")`, which allocates a
slice header plus a backing array for the split parts on every call. For
clients that send conditional requests frequently (S3 GET/HEAD with
`If-None-Match`), this is one avoidable heap allocation per request — pure GC
pressure on a latency-sensitive path.

## What Changes

- In `etagMatches` (`src/transport/s3_communicator.go`):
  - Replace `strings.Split(header, ",")` with a `strings.IndexByte` scan that
    slices the original string for each item (zero copies).
- Preserve parse semantics exactly: whitespace trimming, `*` wildcard, `W/`
  weak-comparison prefix, surrounding double-quote stripping, and the
  comparison against the raw hash.
- Convert `TestS3Communicator_EtagMatches` to a table covering the edge cases
  (quoted, unquoted, weak, `W/` with space, wildcard, wildcard in a list,
  leading/trailing spaces, trailing comma, not-in-list, empty header, empty
  object etag).
- Add `BenchmarkEtagMatches` plus a `BenchmarkEtagMatchesSplit` comparison
  baseline to make the allocation claim reproducible.
- Append a `.jules/bolt.md` learning entry (Rule 44 append-only).

## Non-Goals

- No change to the conditional-request decision logic or the callers
  (`ifRangeMatches`, GET/HEAD handling).
- No wire/protocol or configuration changes.
- Other `strings.Split` sites are out of scope.

## Impact

- **Affected Specs:** `specs/bolt-etag-matches-zero-alloc/spec.md`.
- **Performance:** ETag list parsing goes from 1 alloc/op (80 B) to 0 allocs/op
  (measured ~257–296 ns/op → ~127–181 ns/op on the target host).
- **Correctness:** Parse results are unchanged; all existing transport tests
  must pass, plus the new edge-case table.
