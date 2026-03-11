## 2024-05-24 - [Zero-copy optimizations with io.CopyN]
**Learning:** Using a single `io.CopyN` call with the full file size is significantly faster than manually chunking the read in a loop when handling network file transfers in Go. This is because the standard library can utilize zero-copy system calls (like `splice` or `sendfile`), which reduces memory copying between kernel and user space, and significantly decreases function call overhead.
**Action:** Always prefer a single `io.Copy` or `io.CopyN` call when the total size is known, rather than breaking it down into smaller, fixed-size chunks, especially when reading from network connections to files.
## 2026-03-10 - [Loop invariant code motion in GetMetrics]
**Learning:** Extracting constant boolean checks (like `PolymorphicSystem`) and redundant type conversions (like `time.Duration(cfg.Metrics.Interval) * time.Millisecond`) out of infinite loops reduces branch evaluation and CPU cycles on every loop tick.
**Action:** Always inspect infinite `for` loops or long-running daemons for invariant variables, configuration checks, or repeated mathematical computations that can be hoisted outside the loop to improve steady-state performance.

## 2024-10-24 - Network Protocol Serialization Optimization
**Learning:** Sending multiple small protocol fields sequentially over a network connection using separate string allocations and formatting (`fmt.Sprintf`, `padString`) introduces significant CPU and allocation overhead.
**Action:** In high-frequency network paths, pre-allocate a single byte buffer of the exact packet size. Write fields directly into the buffer using `copy()` and `strconv` instead of string formatting, and rely on the initial zeroed state of the buffer for null-padding. Send the entire buffer in a single `.Write()` call.
