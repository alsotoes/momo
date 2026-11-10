# Tasks — Zero-Allocation /proc Parsing

## Phase 1 — Parsers
- [x] Extract `parseCPUStat([]byte)` — `bytes.IndexByte` line scan, aggregate
      `cpu ` line only.
- [x] Extract `parseMemPercent([]byte)` — colon-delimited labels, `MemTotal` /
      `MemAvailable` compared with `bytes.Equal` against package-level keys.
- [x] `parseDecimal` digit reader with an overflow guard (no `strconv`).
- [x] No `bytes.Fields` and no `string()` conversion anywhere on the sample path.
- [x] Readers keep `common.RecoverErr` (Rule 37) and wrap file errors with `%w`.

## Phase 2 — Tests
- [x] Table-driven `parseCPUStat` cases: iowait present, four fields, no trailing
      newline, missing aggregate line, too few fields, non-numeric, empty.
- [x] Overflow rejected as `EBADMSG`.
- [x] Table-driven `parseMemPercent` cases: half used, missing MemAvailable,
      irrelevant lines, no trailing newline, missing MemTotal, empty.
- [x] `parseDecimal` unit cases including overflow.
- [x] Real `/proc` smoke tests retained; delta sampler test retained.

## Phase 3 — Measurement
- [x] `BenchmarkParseCPUStat` / `BenchmarkParseMemPercent` — `0 B/op, 0 allocs/op`.
- [x] `splitBaseline*` benchmarks record the pre-change algorithm
      (`/proc/stat` 5 allocs → 0; `/proc/meminfo` 24 allocs → 0).

## Phase 4 — Documentation & Compliance
- [x] OpenSpec proposal + spec (Rule 73).
- [x] ADR via `make adr-sync` (Rules 77/78) with a `no-blog` justification.
- [x] `.jules/bolt.md` learning entry (Rule 44), dated to the PR creation date.

## Cross-Phase Requirements
- [x] `go build ./...`, `go test -race ./src/p2p/`, `go vet`, `gofmt -l` clean.
- [x] `make lint` clean (complexity budget).
- [x] `go work vendor` parity (Rule 25).
- [x] `make blog-check`, `make diagram-check`, `make adr-sync-check`,
      `make ai-workflow-check` pass.
