# Tasks: Bolt — zero-allocation ETag header parsing

## Implementation

- [x] Replace `strings.Split(header, ",")` with a `strings.IndexByte` slice scan in `etagMatches` (`src/transport/s3_communicator.go`)
- [x] Preserve parse semantics (trim, `*`, `W/`, quote stripping, raw-hash compare)
- [x] Append `.jules/bolt.md` learning entry (Rule 44)

## Testing

- [x] Convert `TestS3Communicator_EtagMatches` to a table with edge cases (quoted, unquoted, weak, `W/` with space, wildcard, wildcard in list, whitespace, trailing comma, not-in-list, empty header, empty object etag)
- [x] Add `BenchmarkEtagMatches` (0 B/op, 0 allocs/op) and `BenchmarkEtagMatchesSplit` baseline (80 B/op, 1 alloc/op)
- [x] Full transport suite passes (`go test -race ./src/transport/`)
- [x] `go build`, `go vet`, `gofmt` clean

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rule 77/78)
- [x] Blog post (Rule 76)
- [x] PR links tracking issue #1133 (Rule 11)
