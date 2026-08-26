# Change: perf-profiling-baseline — benchmark segments + file-based profiling (Phase-0)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/948

## Why

Momo needs per-pipeline visibility before any performance work (Win1 SIMD
SHA-256, Win2 stray-copy, Win3 buffers/O_DIRECT) so optimization is placed on
data, not guesswork. Today there is **no profile or benchmark surface** for the
dominant storage path: hashing, local blob write, verify-on-read, and S3 spool.
The only benches that exist are micro ones (crush, config). Without a baseline,
the three scaling wins have no `before` to compare against, and a change could
ship regressions undetected.

This change adds the **measurement harness only**. It ships no optimization.

## What

1. **Benchmark segments**, benchstat-compatible and benchmem-aware, recorded to
   `.github/data/benchmark_history.csv` + `docs/PERFORMANCE.md` by the standard
   pre-commit hook / `make benchmark`:
   - `BenchmarkHashBytes` / `BenchmarkHashFile` — SHA-256 content hashing.
   - `BenchmarkLocalWrite` — 64KB-buffered local blobstore write.
   - `BenchmarkReadVerify` — full-object verify-on-read (stream + hash + assert
     content-address).
   - `BenchmarkS3PutSpool` — single-pass spool+hash (`MultiWriter`), no HTTP.
   - Each at 1 MiB / 64 MiB / 256 MiB.
2. **Profiling discipline** (Rule 75): no networked `net/http/pprof`. Profiles
   are captured with Go's built-in file-based flags on the test/bench binary:
   `go test -cpuprofile -memprofile -blockprofile` (plus `-bench`, `-benchmem`).
3. **Steering Rule 75** codifying the above (and extending Rule 74's security
   contract).

## Goals / Non-Goals

- **Goals:** baseline for the three perf wins; establish hashing as the read-path
  hotspot with reproducible numbers; define the safe profiling contract.
- **Non-Goals:** implementing Win1/2/3 (separate branches, gated by this
  baseline). No O_DIRECT, no SIMD, no copy-reduction here. No behavior change to
  production code paths.

## Success Criteria

- Four new benchmark segments present, benchstat-compatible, each with 3 sizes.
- `go test -run='^$' -bench='<segment>' -cpuprofile out.prof` produces a valid
  pprof file with **no network listener** (Rule 75).
- Rule 75 present in `openspec/config.yaml`.
- `make test` green; three-dot diff limited to the new bench files, this OpenSpec
  set, and `config.yaml`.
