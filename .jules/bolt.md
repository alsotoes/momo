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

## 2024-05-24 - Avoiding Custom Parsing Micro-Optimizations
**Learning:** Replacing standard library functions (like `strconv.ParseInt`) with custom byte-slice parsers to save a single allocation sacrifices codebase readability and is highly prone to edge-case bugs (e.g., `math.MinInt64` overflow checks).
**Action:** Stick to standard library functions for parsing. If allocations are a bottleneck, look for higher-level architectural optimizations or safe standard library alternatives (like `strconv.AppendInt` for formatting).

## 2024-05-24 - Preserving Network Protocol Padding
**Learning:** When optimizing string or integer formatting, replacing a custom padding function (like `PadString`) with `strconv.AppendInt` on a pre-allocated zeroed buffer changes the padding behavior (e.g., right-padding instead of left/space padding) and breaks the network protocol.
**Action:** Always fully understand the implementation of any custom padding or serialization functions before attempting to optimize them away.

## 2026-04-29 - [Optimize network handshakes with single write]
**Learning:** When performing sequential network writes during a protocol handshake (like sending an AuthToken followed by a Timestamp), executing separate `conn.Write()` calls for each field incurs multiple system call overheads and potential network delays (e.g., Nagle's algorithm).
**Action:** Always pre-allocate a single byte buffer sized for the combined payload, populate it using `copy()`, and dispatch it with a single `conn.Write()` call to improve throughput and reduce CPU usage.
## 2026-05-01 - Optimize metrics threshold checking
**Learning:** Checking percentage metrics (0-100) against threshold values (0.0-1.0) requires division (e.g. `memUsed / 100`) inside a hot loop. Pre-calculating the thresholds as percentages outside the loop (or at least doing it once and avoiding the division) saves CPU cycles. Furthermore, checking if `currentIndex == -1` before doing heavy lifting and system calls is beneficial. Short-circuiting evaluation when memory usage already triggers an increase avoids reading CPU percent altogether.
**Action:** Always pre-calculate float thresholds to match metric inputs natively and hoist common checks to early-return before executing heavy system metrics calls.

## 2026-05-02 - Eliminate allocations using stack-allocated arrays
**Learning:** Using `make([]byte, 0, N)` or converting literal strings to slices inline `[]byte("ACK")` forces allocations onto the heap because the compiler cannot statically determine their escape profile. This adds measurable overhead and triggers garbage collection.
**Action:** To eliminate heap allocations and garbage collection overhead when formatting strings and integers for frequent network writes, use stack-allocated arrays (e.g., `var buf [32]byte`) sliced dynamically (`buf[:0]`) with `strconv.AppendInt` or `append()`.

## 2026-05-04 - Optimize `checkMetricsAndSwap` by skipping unnecessary CPU percent evaluation
**Learning:** Checking memory usage against a replication threshold usually dictates whether the cluster replication mode scales up, effectively shadowing the result of CPU usage against the same threshold (if one succeeds, we scale up anyway). CPUPercent requires system calls to gather process status, which is expensive in a periodic loop.
**Action:** Always ensure early returns or short-circuits are aggressively applied in recurring metric checking routines where the success state is an OR logical condition between memory and CPU metric checks.

## 2026-05-04 - Fix False-Positive Performance Degradations in CI
**Learning:** Benchmarks measuring sub-10ns operations (like `IndexSearch`) are extremely sensitive to environmental noise in GitHub Actions runners. A `COUNT` of 5 does not generate enough samples to overcome routine variance, often resulting in false-positive performance degradation warnings exceeding 5%.
**Action:** Always increase `benchstat` execution count sampling size (`COUNT=15` or higher) in CI workflows like `.github/workflows/benchmark_compare.yml` when measuring highly sensitive micro-benchmarks to improve statistical confidence and prevent failed builds from noise.

## 2026-05-04 - Optimization Trap: Struct Property Access in Tight Loops
**Learning:** Replacing a direct float division `float64(v.UsedPercent) / 100` with a multiplication of a pre-calculated struct property `cfg.Metrics.MaxThreshold * 100` actually resulted in a measurable performance degradation (up to +16.99%) inside extremely tight, high-frequency loops (like the `checkMetricsAndSwap` loop benchmarking at ~8ns). The overhead of evaluating and comparing against struct properties outweighed the cost of the simple float division on modern CPUs.
**Action:** Do not preemptively replace simple mathematical operations with pre-calculated struct lookups inside highly optimized micro-loops unless profiled directly, as memory access patterns may be slower than ALU computation.

## 2026-05-04 - Fix False-Positive Performance Degradations in CI (Continued)
**Learning:** Even with an increased COUNT (e.g. 15), benchmarks measuring sub-10ns operations (like `IndexSearch`) remain extremely sensitive to environmental noise in GitHub Actions runners. Small variations of < 1 nanosecond can cause > 50% relative performance degradation failures.
**Action:** When a benchmark is measuring < 10 ns execution times and frequently triggers false positives, increasing sample size alone may be insufficient. In such cases, either redesign the benchmark to do more work per iteration (measuring macro-scale latency) or replace manual slice searching (which triggers this issue) with `slices.Index` from the Go standard library, which provides compiler-level optimizations and better benchmark consistency.
## 2026-05-05 - Leverage slices.Index for loop performance
**Learning:** Using `slices.Index` from the Go standard library can be significantly faster (up to ~7x in benchmarks) compared to manually iterating through a slice with a `for range` loop to find a string. The compiler optimization for standard library functions offers better baseline performance than a manual loop implementation.
**Action:** Always prefer using `slices.Index` or `slices.Contains` over custom loop implementations for slice searching, to maximize performance and improve readability.
## 2026-05-07 - Zero-Allocation Handshakes via Stack Arrays
**Learning:** In Go, passing a dynamically allocated slice (e.g., `make([]byte, 64)`) to interface methods like `io.ReadFull(io.Reader, []byte)` forces the slice to escape to the heap, creating garbage collection overhead for every connection.
**Action:** Always use fixed-size stack arrays (e.g., `var buffer [64]byte`) and slice them (`buffer[:]`) when reading small, fixed-length packets (like authentication tokens or fixed-width timestamps) to eliminate heap allocations and improve network throughput on hot paths.

## 2024-05-09 - [Eliminate Heap Allocations for Fixed-Size Network Payloads]
**Learning:** When using `io.ReadFull` to read fixed-size data from a network connection into a buffer, passing a slice created with `make([]byte, ...)` forces that allocation to escape to the heap. This occurs because the interface method `Read(p []byte)` forces the argument to escape in Go's current escape analysis (unless it can prove otherwise, which is often not possible across interface boundaries).
**Action:** To eliminate this recurring heap allocation and garbage collection overhead during high-frequency network operations, explicitly declare a fixed-size array on the stack (e.g., `var buffer [192]byte`) and pass a slice of it (`buffer[:]`) to `io.ReadFull`. This ensures the array remains on the stack while satisfying the `io.Reader` interface. Note: due to the way `var buffer [N]byte` requires a constant for N, ensure `N` is composed of constants. This optimization is particularly beneficial in hot paths like `getMetadata`.
## 2024-05-11 - Do not replace PadString with zero-initialized arrays
**Learning:** While replacing `make([]byte, ...)` with stack allocated arrays (`var buffer [...]byte`) to avoid heap escapes, do not replace the explicit `PadString` method call with a direct `copy()` into a zero-initialized array to try to save the formatting memory allocation overhead. Although `copy()` null-pads correctly, it lacks the logic to truncate the string if the string exceeds the buffer size, which `PadString` safely handles. Automated reviews flag this explicitly as a critical regression.
**Action:** When replacing dynamically allocated byte slices with stack-allocated byte arrays, preserve the `PadString` usage and just copy its output into the new stack array.
## 2026-05-20 - [Optimize Handshake Reads via Stack Buffer]
**Learning:** Performing multiple sequential `io.ReadFull` calls for small, fixed-length protocol headers (like AuthToken and Timestamp) incurs unnecessary system call overhead on every connection.
**Action:** Always combine sequential reads of fixed-size network payloads into a single stack-allocated byte array of their total length and use a single `io.ReadFull` call to reduce system calls and improve performance without causing heap escapes.
## 2026-05-21 - Consolidate Network Writes and Reduce Allocations in Replication Metrics and Server
**Learning:** In Go, repeatedly calling `conn.Write` or using `json.NewEncoder` for small payloads (like authentication tokens + JSON structs) causes unnecessary memory allocations and system call overhead.
**Action:** Consolidate network writes by marshalling JSON first and appending it to a dynamically-sized buffer using `make` and `append`, then sending the unified byte slice via a single `conn.Write` call. This prevents string-to-byte allocation of JSON, the encoder's intermediate buffer allocation, and halves the number of write system calls while maintaining memory safety.
## 2026-05-28 - [Remove redundant reading of configuration AuthToken]
**Learning:** During the parsing of configurations via `loadGlobalConfig`, the `auth_token` string property was fetched and checked for being empty twice. The redundant read operation didn't provide additional functionality, but slightly increased the memory processing time in micro benchmarks.
**Action:** Removed the redundant code that read the `auth_token` value from the `global` section of the configurations again, thereby improving code readability and minimizing configuration parsing overhead.
