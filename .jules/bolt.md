## 2026-03-09 - Go `io.CopyN` loop anti-pattern
**Learning:** Calling `io.CopyN` repeatedly in a small fixed-size loop to transfer network files completely negates the performance benefits of Go's `io` package. It introduces severe overhead (allocations, context switches) and prevents the standard library from utilizing optimized zero-copy system calls like `sendfile` or `splice`.
**Action:** When transferring files of known size over a network connection in Go, always use a single `io.CopyN(dst, src, fileSize)` call rather than chunking it manually in a loop.
