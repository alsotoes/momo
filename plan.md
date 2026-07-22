1. **Optimize S3 Communicator XML Formatting:**
   - In `src/transport/s3_communicator.go`, replace `strconv.Itoa` and `strconv.FormatInt` inside `FormatListObjectsV2XML` with `strconv.AppendInt` to eliminate string heap allocations during serialization. Use a small pre-allocated byte slice buffer (e.g. `var numBuf [32]byte`) for appending integers.
2. **Optimize Momo-TCP & QUIC Packet Serialization:**
   - In `src/transport/momo_tcp.go` and `src/transport/momo_quic.go`, replace `.PadString(strconv.FormatInt(...))` with `strconv.AppendInt` for `timestamp` during `HandshakeClient` / `HandshakeServer` and `file.Size` inside list files padding blocks, in order to avoid heap-allocated padding buffers.
3. **Verify Functionality:**
   - Run `git diff` to ensure changes align with expected improvements.
   - Run the complete testing suite utilizing `make test` or `go test ./...`.
4. **Complete pre-commit steps:**
   - Complete pre-commit steps to ensure proper testing, verification, review, and reflection are done.
5. **Create a Pull Request:**
   - Submit the branch naming it something short and descriptive.
   - PR Title: `⚡ Bolt: [performance improvement] Optimize network packet serialization and eliminate strconv heap allocations`
   - Provide What, Why, Impact, and Measurement in the PR description as per instructions.
