## 2024-03-20 - Fast String Padding in Go
**Learning:** For padding strings with repeating characters (like null bytes) in Go, `strings.Repeat` is significantly faster (~1150ns) and more readable than pre-allocating a `strings.Builder` and writing bytes in a loop (~3200ns), and better than concatenating `string(make([]byte, n))` (~2100ns).
**Action:** Always prefer `strings.Repeat` when building predictable padding sequences in Go to get the best performance without sacrificing code clarity.

## 2024-05-20 - Fast Null-Terminated String Extraction
**Learning:** For parsing fixed-size network buffers representing null-terminated strings, using `bytes.IndexByte(b, 0)` to find the terminator and slicing is significantly faster than using `bytes.Trim(b, "\x00")` or `bytes.TrimRight`. `bytes.Trim` does string allocation, rune decoding, and recursive checks which are expensive operations for fixed-length null-padded byte slices.
**Action:** Always prefer `bytes.IndexByte(buffer, 0)` to find the boundary of null-terminated strings extracted from network packets and simply slice the buffer.

## 2026-03-21 - Optimize getMetadata trimming
**Learning:** `bytes.Trim` recursively checks both ends of a byte slice, causing performance overhead. Since padding is strictly null characters (`\x00`), `bytes.IndexByte(b, 0)` is ~6x faster because it immediately returns the index of the first null character and allows taking a direct slice, reducing operations significantly.
**Action:** Replace `bytes.Trim(b, "\x00")` with a custom inline function utilizing `bytes.IndexByte` and slice manipulation when working with pre-allocated buffer padding.

## 2026-03-26 - Pre-allocate slice capacity
**Learning:** During configuration loading, `append` inside loops without pre-allocated capacity forces the Go runtime to repeatedly allocate new, larger arrays and copy data, creating unnecessary GC pressure and CPU overhead.
**Action:** When parsing comma-separated strings or mapping sections into slices, always initialize the slice with `make([]T, 0, len(items))` if the size is known beforehand. This simple change reduces execution time and allocations.

## 2026-03-22 - Fast Null-Terminated String Parsing in Go
**Learning:** For parsing fixed-size network buffers representing null-terminated strings, using `bytes.IndexByte(b, 0)` to find the terminator and slicing is significantly faster than using `bytes.Trim(b, "\x00")` or `bytes.TrimRight`. It avoids recursive checking and extra string allocations, performing ~2.5x to ~3.5x faster in benchmarks.
**Action:** Always prefer `bytes.IndexByte` to locate the null byte and slice the array when parsing null-padded strings from fixed-size buffers, rather than using `bytes.Trim` or `strings.TrimRight`.

## 2026-03-25 - Slice pre-allocation for parsing strings
**Learning:** In Go, dynamically appending elements to a slice like `var res []int` causes the runtime to reallocate memory and copy elements multiple times as the capacity grows. For string splitting or fixed collections, pre-allocating the capacity via `make([]T, 0, len(elements))` avoids this overhead entirely, improving loop execution time and reducing garbage collection pressure.
**Action:** Always pre-allocate slices when the target length is known beforehand or can be bounded (e.g., after `strings.Split` or iterating over configuration map keys).

## 2026-03-29 - Fast Custom Padding String Parsing
**Learning:** For padding variables, `strconv.ParseInt(string(bytes))` creates unneeded string allocations and conversions. By implementing a custom null-byte padded parsing function directly over the `[]byte` representation, it offers roughly a 40%+ performance boost over standard `strconv.ParseInt` with `string()` conversion, decreasing execution time per loop significantly for fixed-width networking variables.
**Action:** Replace `strconv.ParseInt` with customized byte-level iteration mapping for networking variables where length and type (like ascii digits) and structure (like null-termination) are known ahead of time.

## 2024-03-27 - Fast Configuration Passing in Periodic Loops
**Learning:** In periodic loops or event handlers (like metric loops or health checks), re-parsing configuration files by calling helper functions like `GetConfigFromFile()` on every execution introduces severe file I/O and parsing overhead, dragging down performance and creating unnecessary garbage.
**Action:** Always inject or pass the pre-parsed `Configuration` object down the call stack instead of re-reading it from disk, especially in hot paths and periodic functions.
