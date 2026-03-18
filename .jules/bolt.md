## 2024-03-20 - Fast String Padding in Go
**Learning:** For padding strings with repeating characters (like null bytes) in Go, `strings.Repeat` is significantly faster (~1150ns) and more readable than pre-allocating a `strings.Builder` and writing bytes in a loop (~3200ns), and better than concatenating `string(make([]byte, n))` (~2100ns).
**Action:** Always prefer `strings.Repeat` when building predictable padding sequences in Go to get the best performance without sacrificing code clarity.
