## 2024-03-20 - Fast String Padding in Go
**Learning:** For padding strings with repeating characters (like null bytes) in Go, `strings.Repeat` is significantly faster (~1150ns) and more readable than pre-allocating a `strings.Builder` and writing bytes in a loop (~3200ns), and better than concatenating `string(make([]byte, n))` (~2100ns).
**Action:** Always prefer `strings.Repeat` when building predictable padding sequences in Go to get the best performance without sacrificing code clarity.

## 2026-03-22 - Fast Null-Terminated String Parsing in Go
**Learning:** For parsing fixed-size network buffers representing null-terminated strings, using `bytes.IndexByte(b, 0)` to find the terminator and slicing is significantly faster than using `bytes.Trim(b, "\x00")` or `bytes.TrimRight`. It avoids recursive checking and extra string allocations, performing ~2.5x to ~3.5x faster in benchmarks.
**Action:** Always prefer `bytes.IndexByte` to locate the null byte and slice the array when parsing null-padded strings from fixed-size buffers, rather than using `bytes.Trim` or `strings.TrimRight`.
