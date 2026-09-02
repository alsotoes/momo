# 0029-perf-profiling-baseline

## Status
Proposed

## Confidence
Low

## Context
Momo needs per-pipeline visibility before any performance work (Win1 SIMD
SHA-256, Win2 stray-copy, Win3 buffers/O_DIRECT) so optimization is placed on
data, not guesswork. Today there is **no profile or benchmark surface** for the
dominant storage path: hashing, local blob write, verify-on-read, and S3 spool.
The only benches that exist are micro ones (crush, config). Without a baseline,
the three scaling wins have no `before` to compare against, and a change could
ship regressions undetected.

This change adds the **measurement harness only**. It ships no optimization.

## Decision
- benchmark segments: The following benchmarks MUST exist, be `-benchmem`-compatible, report `b.SetBytes`, and run at 1 MiB / 64 MiB / 256 MiB as sub-benchmarks:
- file-based profiling contract: A profile capture MUST be runnable with no network listener:
- baseline recording: Segment results MUST be recorded through the standard benchmark pipeline: `make benchmark` / pre-commit hook regenerating `.github/data/benchmark_history.csv` and `docs/PERFORMANCE.md`; no bespoke recording harness. ## Success Criteria - All four segments present under `src/common/hash_bench_test.go` and `src/storage/bench_test.go`, benchstat-compatible, three sizes each. - Profile capture proven via built-in flags with no network listener. - Rule 75 present in `openspec/config.yaml`. - `make te...

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Planned
- **Tests**: Partial
- **Docs**: Planned
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/perf-profiling-baseline/
- Blog: docs/blog/posts/...md
