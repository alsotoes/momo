# Tasks: Bolt — zero-allocation AWS chunked header parsing

## Implementation

- [x] Replace `strings.Split(line, ";")` with a `strings.IndexByte` slice scan in `parseAWSChunkHeader` (`src/transport/aws_chunked.go`)
- [x] Replace `strings.TrimPrefix(part, awsChunkSigField)` with a length-bound slice
- [x] Append `.jules/bolt.md` learning entry (Rule 44)

## Testing

- [x] Extend `TestParseAWSChunkHeader` with an edge-case table (no `;`, trailing `;`, `;;`, extension fields, whitespace, multiple signatures, empty sig/size, non-hex, negative)
- [x] Add `BenchmarkParseAWSChunkHeader` (0 B/op, 0 allocs/op vs baseline 32 B/op, 1 alloc/op)
- [x] Full transport suite passes (`go test -race ./src/transport/`)
- [x] `go build`, `go vet`, `gofmt` clean

## Compliance

- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rule 77/78)
- [x] Blog post (Rule 76)
- [x] PR links tracking issue #1113 (Rule 11)
