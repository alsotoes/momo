## 2024-03-20 - Fast String Padding in Go
**Learning:** For padding strings with repeating characters (like null bytes) in Go, `strings.Repeat` is significantly faster (~1150ns) and more readable than pre-allocating a `strings.Builder` and writing bytes in a loop (~3200ns), and better than concatenating `string(make([]byte, n))` (~2100ns).
**Action:** Always prefer `strings.Repeat` when building predictable padding sequences in Go to get the best performance without sacrificing code clarity.

## 2026-03-21 - Optimize getMetadata trimming
**Learning:** `bytes.Trim` recursively checks both ends of a byte slice, causing performance overhead. Since padding is strictly null characters (`\x00`), `bytes.IndexByte(b, 0)` is ~6x faster because it immediately returns the index of the first null character and allows taking a direct slice, reducing operations significantly.
**Action:** Replace `bytes.Trim(b, "\x00")` with a custom inline function utilizing `bytes.IndexByte` and slice manipulation when working with pre-allocated buffer padding.

## 2026-03-25 - Slice pre-allocation for parsing strings
**Learning:** In Go, dynamically appending elements to a slice like `var res []int` causes the runtime to reallocate memory and copy elements multiple times as the capacity grows. For string splitting or fixed collections, pre-allocating the capacity via `make([]T, 0, len(elements))` avoids this overhead entirely, improving loop execution time and reducing garbage collection pressure.
**Action:** Always pre-allocate slices when the target length is known beforehand or can be bounded (e.g., after `strings.Split` or iterating over configuration map keys).
