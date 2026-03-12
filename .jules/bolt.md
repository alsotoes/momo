## 2024-05-24 - [Zero-copy optimizations with io.CopyN]
**Learning:** Using a single `io.CopyN` call with the full file size is significantly faster than manually chunking the read in a loop when handling network file transfers in Go. This is because the standard library can utilize zero-copy system calls (like `splice` or `sendfile`), which reduces memory copying between kernel and user space, and significantly decreases function call overhead.
**Action:** Always prefer a single `io.Copy` or `io.CopyN` call when the total size is known, rather than breaking it down into smaller, fixed-size chunks, especially when reading from network connections to files.
## 2026-03-10 - [Loop invariant code motion in GetMetrics]
**Learning:** Extracting constant boolean checks (like `PolymorphicSystem`) and redundant type conversions (like `time.Duration(cfg.Metrics.Interval) * time.Millisecond`) out of infinite loops reduces branch evaluation and CPU cycles on every loop tick.
**Action:** Always inspect infinite `for` loops or long-running daemons for invariant variables, configuration checks, or repeated mathematical computations that can be hoisted outside the loop to improve steady-state performance.

## 2026-03-11 - Optimize file sending with io.Copy
**Learning:** Using io.Copy leverages Go's standard library to potentially use zero-copy operations like sendfile, avoiding user-space buffer allocations and manual loop overhead.
**Action:** Use io.Copy or io.CopyN instead of manual byte buffer reading/writing loops when transferring streams of data.

## 2024-05-25 - [Single-buffer metadata serialization]
**Learning:** Formatting string fields and sending them in multiple `.Write()` calls over a network connection causes unnecessary memory allocations and slow system call overhead. Pre-allocating a single byte buffer of the exact network packet size and using `copy()` and `strconv.AppendInt` drastically reduces execution time and allocations.
**Action:** When transmitting simple protocol headers or fields over TCP, allocate a single `[]byte` slice for the expected packet size and map data into it sequentially before dispatching via a single `.Write()` to optimize memory usage and avoid syscall limits.
