## 2024-05-24 - [Zero-copy optimizations with io.CopyN]
**Learning:** Using a single `io.CopyN` call with the full file size is significantly faster than manually chunking the read in a loop when handling network file transfers in Go. This is because the standard library can utilize zero-copy system calls (like `splice` or `sendfile`), which reduces memory copying between kernel and user space, and significantly decreases function call overhead.
**Action:** Always prefer a single `io.Copy` or `io.CopyN` call when the total size is known, rather than breaking it down into smaller, fixed-size chunks, especially when reading from network connections to files.
## 2026-03-10 - [Loop invariant code motion in GetMetrics]
**Learning:** Extracting constant boolean checks (like `PolymorphicSystem`) and redundant type conversions (like `time.Duration(cfg.Metrics.Interval) * time.Millisecond`) out of infinite loops reduces branch evaluation and CPU cycles on every loop tick.
**Action:** Always inspect infinite `for` loops or long-running daemons for invariant variables, configuration checks, or repeated mathematical computations that can be hoisted outside the loop to improve steady-state performance.

## 2026-03-21 - Optimize getMetadata trimming
**Learning:** `bytes.Trim` recursively checks both ends of a byte slice, causing performance overhead. Since padding is strictly null characters (`\x00`), `bytes.IndexByte(b, 0)` is ~6x faster because it immediately returns the index of the first null character and allows taking a direct slice, reducing operations significantly.
**Action:** Replace `bytes.Trim(b, "\x00")` with a custom inline function utilizing `bytes.IndexByte` and slice manipulation when working with pre-allocated buffer padding.

## 2026-03-25 - Pre-allocate ReplicationOrder slice
**Learning:** Pre-allocating a slice's capacity using `make([]T, 0, len)` when the final size is known (e.g., after `strings.Split`) significantly reduces memory allocations and improves performance. In `loadGlobalConfig`, this change reduced allocations from 7 to 2 and improved latency by ~38%.
**Action:** Always pre-allocate slice capacity with `make` when the number of elements can be determined beforehand to avoid multiple re-allocations and copying during `append`.
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

## 2024-03-27 - Fast Configuration Passing in Periodic Loops
**Learning:** In periodic loops or event handlers (like metric loops or health checks), re-parsing configuration files by calling helper functions like `GetConfigFromFile()` on every execution introduces severe file I/O and parsing overhead, dragging down performance and creating unnecessary garbage.
**Action:** Always inject or pass the pre-parsed `Configuration` object down the call stack instead of re-reading it from disk, especially in hot paths and periodic functions.

## 2026-04-01 - Fast Integer Formatting in Go
**Learning:** For formatting a single integer as a string, `strconv.Itoa` is significantly faster (~19x) and generates fewer memory allocations than `fmt.Sprintf("%d", ...)`. `fmt.Sprintf` uses reflection and a more complex parsing logic, which is overkill for simple integer-to-string conversions.
**Action:** Always prefer `strconv.Itoa` or `strconv.FormatInt` over `fmt.Sprintf` when converting a single integer to its string representation in Go.

## 2026-04-25 - Prevent Wrap-Around in Fast Integer Parsing
**Learning:** When writing custom integer parsing functions in Go to avoid allocations (like `parsePaddedIntFast` reading from `[]byte`), checking `res > (1<<63-1)/10` is insufficient for `int64` overflow protection. It misses wrap-arounds on the final digit.
**Action:** Always include a check for the final digit: `if res == (1<<63-1)/10 && int64(c-'0') > (1<<63-1)%10` to correctly return `strconv.ErrRange`.

## 2026-04-25 - strconv.ParseInt Optimization Insight
**Learning:** In modern Go, `strconv.ParseInt(string(b[:i]), 10, 64)` is compiler-optimized and does not allocate strings on the heap, so rewriting it purely to remove allocations is unnecessary. However, a custom inline loop still avoids the overhead of function calls and generalized base-10 parsing logic, proving ~2x faster.
**Action:** When pursuing byte-level integer parsing optimizations in performance-critical network paths, measure speed, not just allocations, as custom parsing can reduce CPU time significantly even if allocations are already zero.

## 2026-04-26 - [Fast integer parsing from byte slice]
**Learning:** Parsing numeric byte slices from network buffers by converting to a string (`strconv.ParseInt(string(bytes))`) incurs unnecessary heap allocations. A custom fast parser iterating over the byte slice directly eliminates this overhead.
**Action:** Always parse null-padded network byte slices using direct byte-level iteration mapping when reading simple integers like timestamps or sizes, avoiding string conversions. Ensure custom parsers correctly handle empty bounds, stop at null bytes, and include appropriate integer overflow safety checks.

## 2026-04-27 - Hoist Hash Computation in Concurrent File Replication
**Learning:** In network file replication, computing the file's SHA-256 hash inside each concurrent connection's goroutine (`sendFile`) causes redundant, $O(N)$ CPU-intensive hashing and disk I/O per file.
**Action:** Always pre-compute file metadata (like hashes and sizes) before entering concurrent transmission loops, passing the pre-computed metadata to each goroutine.
