## 2026-03-18 - Optimized padString implementation
**Learning:** In Go, concatenating a string with a newly created byte slice cast to a string (e.g., `input + string(make([]byte, n))`) results in multiple redundant allocations and copies.
**Action:** Use `strings.Builder` with `Grow(n)` to pre-allocate the required buffer and `WriteByte(0)` in a loop to append null bytes efficiently. This reduces allocations to a single one and avoids intermediate string objects.
