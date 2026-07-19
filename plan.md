1. Add a highly optimized string extraction function `TrimNullBytesString(b []byte) string` to `src/common/string.go`.
   - The logic will use `bytes.IndexByte(b, 0)` to find the first null byte. If found, it returns `unsafe.String(unsafe.SliceData(b), idx)`. Otherwise, it returns the whole slice `unsafe.String(unsafe.SliceData(b), len(b))`. This eliminates string allocation overhead.
2. Replace all instances of `string(bytes.TrimRight(fileBuf[:], "\x00"))` in `src/transport/momo_tcp.go` and `src/transport/momo_quic.go` with `common.TrimNullBytesString(fileBuf[:])`.
3. Update corresponding unit tests in `src/transport/momo_tcp_test.go` and `src/transport/factory_test.go`.
   - Replace instances like `string(bytes.TrimRight(packet[0:64], "\x00"))` with `common.TrimNullBytesString(packet[0:64])`.
   - Replace `string(bytes.TrimRight(respBuf[1:65], "\x00"))` with `common.TrimNullBytesString(respBuf[1:65])`.
4. Use `git diff` to confirm the changes across all modified files were applied correctly.
5. Record the learning in `.jules/bolt.md`.
   - Add a new entry with Title: `[Zero-Allocation String Trimming]`.
   - Learning: Using `bytes.TrimRight` recursively checks both ends and requires casting to string, which causes heap allocations. `bytes.IndexByte` followed by `unsafe.String` eliminates allocations and reduces CPU overhead.
   - Action: When trimming padding (e.g. null bytes) from fixed-size byte slices, prefer `bytes.IndexByte` and `unsafe.String` to avoid allocation overhead.
6. Run the full test suite (`cd src && go test ./common ./transport`) to ensure the changes are correct and have not introduced regressions.
7. Complete pre-commit steps to ensure proper testing, verification, review, and reflection are done.
8. Submit a Pull Request.
   - Title: "⚡ Bolt: Zero-Allocation String Trimming"
   - Description must contain What, Why, Impact, and Measurement.
