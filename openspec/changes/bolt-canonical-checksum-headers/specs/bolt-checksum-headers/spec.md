# Bolt Checksum Header Lookups

## Purpose

Keep checksum-header discovery on the S3 ingest path allocation-free.

## Requirements

### Requirement: Zero-Allocation Header Probe

The gateway MUST resolve a request's checksum algorithm without allocating memory
per request, including when no checksum header is present.

#### Scenario: No checksum supplied
- **GIVEN** a PUT request with no `x-amz-checksum-*` header
- **WHEN** `parseChecksum` runs
- **THEN** it returns an empty algorithm and no error with 0 allocations.

#### Scenario: Checksum supplied
- **GIVEN** a PUT request carrying `x-amz-checksum-sha256`
- **WHEN** `parseChecksum` runs
- **THEN** it returns `sha256` and the supplied value.

### Requirement: Behaviour Preservation

The probe MUST keep the established algorithm precedence and error semantics.

#### Scenario: Conflicting explicit algorithm
- **GIVEN** a request with `x-amz-checksum-sha256` and
  `x-amz-checksum-algorithm: crc32`
- **WHEN** `parseChecksum` runs
- **THEN** it returns a deterministic conflict error.

### Requirement: Measurable Savings

A benchmark MUST demonstrate the allocation difference so the optimization can be
re-verified.

#### Scenario: Benchmark comparison
- **GIVEN** the transport benchmarks
- **WHEN** `BenchmarkParseChecksum` and `BenchmarkParseChecksumDynamicBaseline` run
- **THEN** the first reports 0 B/op and the baseline reports a non-zero allocation
  count.
