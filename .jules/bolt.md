## 2024-03-20 - Fast String Padding in Go
**Learning:** For padding strings with repeating characters (like null bytes) in Go, `strings.Repeat` is significantly faster (~1150ns) and more readable than pre-allocating a `strings.Builder` and writing bytes in a loop (~3200ns), and better than concatenating `string(make([]byte, n))` (~2100ns).
**Action:** Always prefer `strings.Repeat` when building predictable padding sequences in Go to get the best performance without sacrificing code clarity.

## 2024-05-20 - Fast Null-Terminated String Extraction
**Learning:** For parsing fixed-size network buffers representing null-terminated strings, using `bytes.IndexByte(b, 0)` to find the terminator and slicing is significantly faster than using `bytes.Trim(b, "\x00")` or `bytes.TrimRight`. `bytes.Trim` does string allocation, rune decoding, and recursive checks which are expensive operations for fixed-length null-padded byte slices.
**Action:** Always prefer `bytes.IndexByte(buffer, 0)` to find the boundary of null-terminated strings extracted from network packets and simply slice the buffer.
