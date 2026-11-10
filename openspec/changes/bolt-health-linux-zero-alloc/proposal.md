# Change: Zero-Allocation /proc Parsing in the Health Monitor

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1312 (tracking, opened for PR #1311)
- https://github.com/alsotoes/momo/issues/1311 (original PR, rebuilt)

## Why

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

## What Changes

- Extract the pure parsers `parseCPUStat([]byte)` and `parseMemPercent([]byte)`.
  They scan the raw bytes: line splitting with `bytes.IndexByte`, field scanning
  with an in-place separator/digit walk (`parseDecimal`, with an overflow guard),
  and label comparison with `bytes.Equal` against package-level `[]byte` keys.
  Neither function converts bytes to strings, so both are allocation-free.
- Keep `readCPUStat`/`readMemPercent` as thin readers around the parsers, with
  `common.RecoverErr` (Rule 37) instead of hand-rolled recover blocks.
- Preserve error causes: file errors are wrapped with `%w` (so `errors.Is(err,
  fs.ErrNotExist)` keeps working), and format errors map to `syscall.EBADMSG`,
  a missing `cpu` line to `syscall.ENOENT`.
- Tests now cover the parsers with synthetic `/proc` content (format edge cases,
  overflow, missing keys) instead of relying only on the host's real `/proc`.
- Benchmarks record both the new parsers and the pre-change algorithm, so the
  win is measured rather than asserted.

## Impact

- Affected specs: `bolt-health-linux-zero-alloc` (new).
- Affected code: `src/p2p/health_linux.go`, `src/p2p/health_linux_test.go`.
- Behavior: identical. CPU still uses idle + iowait as non-busy, the first sample
  still reports 0% CPU (delta), and a meminfo without `MemAvailable` still counts
  all memory as used.
- Measured: `/proc/stat` 1324 ns/1840 B/5 allocs → 484 ns/0 B/0 allocs;
  `/proc/meminfo` 3299 ns/1888 B/24 allocs → 749 ns/0 B/0 allocs.
