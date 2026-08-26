> GitHub Issue URL: https://github.com/alsotoes/momo/issues/948

# perf-profiling-baseline Specification

## Purpose
Provide a reproducible benchmark + profiling baseline for the dominant storage
path (hash, local write, verify-on-read, S3 spool) and codify a security-safe
profiling contract, so the three scaling wins can be placed on data and
measured before/after without introducing networked pprof.

## Steering Rule

### SR1: Rule 75 — no networked pprof on unauthenticated listeners
Profiling MUST be captured with Go's built-in **file-based** test flags
(`go test -cpuprofile X -memprofile X -blockprofile X [-bench . -benchmem]`),
producing `.pprof` files. A `net/http/pprof` listener (or any HTTP debug
endpoint exposing profiler runtime) MUST NOT be bound on the unauthenticated
data-path listener; because momo has no auth/TLS (config.yaml), a networked
pprof endpoint is a remote-code-execution-class surface. Any future admin
profiling endpoint MUST be a dedicated listener bound to loopback/Unix socket
only, explicitly enabled at boot, never default-on, and TLS/mTLS-protected
whenever it leaves loopback. This extends Rule 74's security contract.

## Requirements

### Requirement: benchmark segments
The following benchmarks MUST exist, be `-benchmem`-compatible, report
`b.SetBytes`, and run at 1 MiB / 64 MiB / 256 MiB as sub-benchmarks:

#### Scenario: hashing segment
- `BenchmarkHashBytes/<size>` — `common.HashBytes` (in-memory SHA-256).
- `BenchmarkHashFile/<size>` — `common.HashFile` (streamed on-disk SHA-256).

#### Scenario: local write segment
- `BenchmarkLocalWrite/<size>` — `LocalBlobStore.PutBlob` over a temp dir,
  64KB-buffered write; a unique hash per iteration forces a fresh path write.

#### Scenario: verify-on-read segment
- `BenchmarkReadVerify/<size>` — a `verifyingReader` streaming a full in-memory
  blob through the content hash and asserting the expected content-address,
  replicating the current per-read SHA-256 cost.

#### Scenario: S3 spool segment
- `BenchmarkS3PutSpool/<size>` — single-pass `os.CreateTemp` +
  `io.MultiWriter(spill, hasher)` over a size-bounded reader (no HTTP round
  trip).

### Requirement: file-based profiling contract
A profile capture MUST be runnable with no network listener:

#### Scenario: CPU/heap profile via built-ins
- `go test ./src/common ./src/storage -run '^$' -bench 'Hash|LocalWrite|ReadVerify|S3PutSpool' -cpuprofile /tmp/cpu.prof -memprofile /tmp/mem.prof -benchmem`
  MUST produce valid pprof files and bind no listener.
- `go tool pprof /tmp/cpu.prof` MUST work for inspection.

### Requirement: baseline recording
Segment results MUST be recorded through the standard benchmark pipeline:
`make benchmark` / pre-commit hook regenerating `.github/data/benchmark_history.csv`
and `docs/PERFORMANCE.md`; no bespoke recording harness.

## Success Criteria

- All four segments present under `src/common/hash_bench_test.go` and
  `src/storage/bench_test.go`, benchstat-compatible, three sizes each.
- Profile capture proven via built-in flags with no network listener.
- Rule 75 present in `openspec/config.yaml`.
- `make test` green; PR ships `Resolves #948`; three-dot diff contains only the
  new benches, this OpenSpec set, and `config.yaml`.
- Initial baseline numbers recorded on issue #948 for use by Win1/2/3.
