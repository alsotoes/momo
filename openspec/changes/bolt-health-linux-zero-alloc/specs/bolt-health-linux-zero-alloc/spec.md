> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1312

# bolt-health-linux-zero-alloc Specification

## Purpose

Keep the Linux health sampler's per-tick cost proportional to the bytes it reads
and free of heap allocations, while preserving the exact health semantics the
coordination handoff depends on.

## ADDED Requirements

### Requirement: Parsing /proc allocates nothing

`parseCPUStat` and `parseMemPercent` SHALL parse `/proc/stat` and
`/proc/meminfo` content in place and SHALL NOT allocate, as proven by
`BenchmarkParseCPUStat` and `BenchmarkParseMemPercent` reporting
`0 B/op, 0 allocs/op`.

#### Scenario: CPU sample is allocation-free
- **WHEN** `BenchmarkParseCPUStat` runs
- **THEN** it reports `0 B/op` and `0 allocs/op`.

#### Scenario: memory sample is allocation-free
- **WHEN** `BenchmarkParseMemPercent` runs
- **THEN** it reports `0 B/op` and `0 allocs/op`.

### Requirement: Health semantics are unchanged

The parsers SHALL preserve the previous behaviour: the aggregate `cpu ` line is
used (never per-core lines), `idle` and `iowait` are treated as non-busy, the
first sample reports 0% CPU because it has no delta, and meminfo without
`MemAvailable` counts all memory as used.

#### Scenario: aggregate line selected
- **WHEN** both `cpu` and `cpu0` lines are present
- **THEN** only the aggregate `cpu ` line contributes to busy/total.

#### Scenario: iowait is not busy
- **WHEN** the line has five or more fields
- **THEN** both `idle` and `iowait` are subtracted from the total.

#### Scenario: missing MemAvailable
- **WHEN** `/proc/meminfo` content has `MemTotal` but no `MemAvailable`
- **THEN** the reported utilisation is 100%.

### Requirement: Malformed input fails with a POSIX error

The parsers SHALL reject malformed content rather than guessing: a non-numeric
or overflowing field and a missing `MemTotal` map to `syscall.EBADMSG`, and a
`/proc/stat` without an aggregate `cpu ` line maps to `syscall.ENOENT`.

#### Scenario: overflowing counter
- **WHEN** a CPU field exceeds `uint64`
- **THEN** parsing fails with `syscall.EBADMSG` instead of wrapping.

#### Scenario: no aggregate CPU line
- **WHEN** the content has only per-core lines
- **THEN** parsing fails with `syscall.ENOENT`.

### Requirement: Readers keep the cause of file errors

`readCPUStat` and `readMemPercent` SHALL wrap `os.ReadFile` failures with `%w`
so `errors.Is(err, fs.ErrNotExist)` keeps working, and SHALL guard panics with
`common.RecoverErr` (Rule 37) rather than a hand-rolled `recover()`.

#### Scenario: missing /proc entry
- **WHEN** the file cannot be read
- **THEN** the returned error wraps the original and remains matchable.

#### Scenario: panic is contained
- **WHEN** the probe panics
- **THEN** the sampler reports an error and the daemon keeps running.
