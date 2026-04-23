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

## 2026-03-13 - Optimize Network Reads
**Learning:** Pre-allocating single buffers for network I/O reduces system calls and memory allocations, resulting in faster and more efficient network communication in Go. We applied this pattern to replace multiple smaller `io.ReadFull` calls with a single chunked read.
**Action:** Always pre-allocate network buffer slices exactly according to protocol specifications where field lengths are fixed and read all components via a single `io.ReadFull()` or `io.Write()` call.

## 2026-03-16 - [Simultaneous hashing with io.TeeReader]
**Learning:** Using `io.TeeReader` to hash a stream of data as it's being written to disk eliminates the need to read the file from disk a second time to compute its checksum. This halves the total disk I/O during file reception. Since the network connection is already wrapped with a timeout reader (defeating zero-copy `splice`), the user-space routing adds no penalty.
**Action:** Always compute checksums or metrics on the fly using `io.TeeReader` when streaming data to storage, avoiding redundant disk reads.

## 2026-03-18 - Optimized padString implementation
**Learning:** In Go, concatenating a string with a newly created byte slice cast to a string (e.g., `input + string(make([]byte, n))`) results in multiple redundant allocations and copies.
**Action:** Use `make([]byte, length)` followed by `copy(b, input)` and `string(b)` to perform padding efficiently. This leverages the fact that `make` already zeros the slice and reduces the operation to a single allocation and a zero-copy-ish string conversion.
