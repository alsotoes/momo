# 0085-bolt-health-linux-zero-alloc

## Status
Accepted

## Confidence
High

## Context
The Linux health sampler reads `/proc/stat` and `/proc/meminfo` on every
`HealthMonitor` tick to decide whether the node should resign coordination.
The original implementation built three throwaway values per line
(`string(data)`, `strings.Split`, `strings.Fields`) and then converted each field
back to a string for `strconv`, so a single sample cost **5 allocations for the
CPU line and 24 for meminfo on top of the file contents**.

The service is not allocation-bound at large, but the sampler runs on a timer for
the life of the daemon and its cost is pure garbage: nothing it allocates survives
one iteration. Removing it is free correctness-wise and makes the sample
measurable per-call.

The original PR (#1311) proposed the change but stopped halfway: it kept
`bytes.Fields` (which allocates the field slice) and `strconv.ParseUint(string(f))`
(which allocates a string per field), so its "zero-allocation" claim was false, and
it hand-rolled two `recover()` blocks instead of the project's `common.RecoverErr`
helper.

## Decision
- Parsing /proc allocates nothing: `parseCPUStat` and `parseMemPercent` SHALL parse `/proc/stat` and `/proc/meminfo` content in place and SHALL NOT allocate, as proven by `BenchmarkParseCPUStat` and `BenchmarkParseMemPercent` reporting `0 B/op, 0 allocs/op`.
- Health semantics are unchanged: The parsers SHALL preserve the previous behaviour: the aggregate `cpu ` line is used (never per-core lines), `idle` and `iowait` are treated as non-busy, the first sample reports 0% CPU because it has no delta, and meminfo without `MemAvailable` counts all memory as used.
- Malformed input fails with a POSIX error: The parsers SHALL reject malformed content rather than guessing: a non-numeric or overflowing field and a missing `MemTotal` map to `syscall.EBADMSG`, and a `/proc/stat` without an aggregate `cpu ` line maps to `syscall.ENOENT`.
- Readers keep the cause of file errors: `readCPUStat` and `readMemPercent` SHALL wrap `os.ReadFile` failures with `%w` so `errors.Is(err, fs.ErrNotExist)` keeps working, and SHALL guard panics with `common.RecoverErr` (Rule 37) rather than a hand-rolled `recover()`.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Done
- **Blog post**: 

## References
- Issue: #1312
- PR: 
- Spec: `openspec/changes/bolt-health-linux-zero-alloc/`
- Blog: 

