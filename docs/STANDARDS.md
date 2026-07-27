# Momo's Foundational Standards: ⚡ Bolt & 🛡️ Sentinel

To maintain Momo's reputation as a high-performance, ultra-secure distributed storage playground, all code contributions must strictly adhere to our dual architectural standards: **⚡ Bolt (Performance)** and **🛡️ Sentinel (Security)**.

This reference manual codifies the exact rules, engineering rationale, and code-level examples of both standards.

---

## ⚡ Bolt: The Performance Standards

The **⚡ Bolt** standard represents Momo's commitment to high CPU throughput, predictable latency, and zero-overhead memory management in Go. 

Any code marked under **⚡ Bolt** must avoid GC (Garbage Collector) pauses and cache misses.

### 1. Zero-Allocation Strategy (Rule 19)
Go's Garbage Collector introduces unpredictable tail-latencies. To achieve sub-microsecond throughput, code in hot-paths MUST NOT escape variables to the heap.
- **Rule:** Avoid converting byte slices to strings (which triggers a heap allocation).
- **Rule:** Use pre-allocated, stack-bound byte arrays (e.g., `[256]byte`) instead of calling `make([]byte, size)` inside hot loops.
- **Example:** Our optimized `PadString` and `CrushOptimized` algorithms use stack-allocated buffers, reducing allocation overhead from **164 B/op to 0 B/op**.

### 2. Bitwise Amortization of System Calls
System calls (like `SetDeadline` or `Write`) carry high kernel context-switch costs.
- **Rule (Bitwise Deadline Amortization):** Rather than calling `SetDeadline` on every single packet read/write, maintain a counter and only call the kernel syscall once every $N$ operations (e.g., once every 128 chunks), amortizing the syscall cost by over 98%.
- **Rule (Consolidated I/O):** Batch independent protocol metadata and payloads into a single, contiguous byte buffer before invoking a network `Write` syscall to avoid Nagle's delays.

### 3. Bitwise and Integer-Only Algorithmics
Reflection and floating-point math are CPU-intensive.
- **Rule:** Never use Go's reflection (`reflect` package) or JSON/XML standard library encoders (which rely on reflection) inside data-transfer hot paths.
- **Rule:** Build deterministic algorithms (like `CRUSH-lite`) utilizing bitwise shifts, integer multiplication, and direct byte manipulation. This allows the CPU to execute code purely on local hardware registers in under **250 nanoseconds**.

---

## 🛡️ Sentinel: The Defensive Security Standards

The **🛡️ Sentinel** standard represents Momo's zero-trust approach to data handling and network interaction. 

It assumes all connected clients, peer nodes, and incoming network packets are potentially malicious or corrupted.

### 1. Defensive Boundary Bounds Validation (Rule 32)
Untrusted clients can execute Memory-Bloat or Buffer-Overflow Denial of Service (DoS) attacks by sending oversized metadata.
- **Rule:** Always validate and enforce strict byte limits on incoming metadata before allocating memory buffers or executing database transactions.
- **Limit:** Dynamic metadata strings (like Object Names or SHA-256 Hashes) MUST be validated to be **at most 64 bytes**.
- **Limit:** Handshake tokens MUST be validated to be **exactly 64 bytes**. Any payload exceeding these limits is immediately aborted with a POSIX error.

### 2. Sandbox Path-Traversal Mitigation (Rule 10)
A malicious client can attempt to read or overwrite critical host system files (e.g., `/etc/passwd`) by sending object paths containing relative dot-dot segments (e.g., `../../etc/passwd`).
- **Rule:** All incoming object keys MUST be sanitized, and any occurrence of `..` or leading slashes must be stripped or rejected with a standard `syscall.EACCES` (Permission Denied) POSIX error code before hitting the filesystem storage driver.

### 3. CRLF Log Injection Protection (Rule 9)
Malicious clients can inject Carriage Return (`\r`) and Line Feed (`\n`) characters into file metadata to forge syslog entries or corrupt server terminal logs.
- **Rule:** All dynamic strings written to logs or stdout must pass through a strict sanitization function that escapes or replaces CRLF control characters with safe placeholders.

### 4. Continuous Progressive Deadlines (Rule 24)
To defend against Slowloris attacks (where clients open connections and trickle data at 1 byte/second to exhaust thread pools), Momo enforces progressive, phased deadlines:
- **Handshake Phase:** Strictly bounded to at most **10 seconds**.
- **Metadata Phase:** Strictly bounded to at most **60 seconds**.
- **Data Transfer Phase:** Progressively adjusted based on active network throughput, closing stale connections automatically.

### 5. Scanner-Safe Test Secrets (Rule 29)
Automated secret scanners (e.g., GitHub Secret Scanning, TruffleHog) may flag dummy tokens used in tests, configs, and CI scripts as real secrets, triggering false-positive alerts.
- **Rule:** All dummy tokens (e.g., `a1b2c3d4e5f6...`, `super_secret_token`, `secret`) MUST be annotated with a trailing `// notsecret` (Go code) or `# notsecret` (shell/config) comment on the same line.
- **Enforcement:** The `.github/scripts/check-notsecret.sh` script scans the codebase for known dummy token patterns without the `notsecret` annotation. It runs in both the pre-commit hook (`hooks/pre-commit`) and CI (`.github/workflows/go.yml`).
- **CI Step:** "Check Scanner-Safe Secrets (Rule 29)" in the Go workflow.

### 6. Connection Concurrency Limit (Rule 32b)
To prevent resource exhaustion via connection flooding, the server enforces a maximum concurrent connection limit.
- **Rule:** The daemon accepts at most **1000 concurrent connections** via a semaphore (`maxConcurrentConnections = 1000` in `server.go`). Connections exceeding this limit block until a slot is freed.
- **S3 Bounded Reads:** HTTP request reads are limited to **65536 bytes** via `LimitedConnReader` to prevent memory bloat from oversized S3 request headers.

### 7. P2P Safety Limits
To prevent CPU and memory exhaustion from malicious or buggy peers, the P2P subsystem enforces strict limits on incoming gossip data:
- **Heartbeat Peer Count:** Each heartbeat message carries at most `MaxPeersInHeartbeat = 256` peer entries. Heartbeats exceeding this limit are truncated and `E2BIG` is logged.
- **Payload Size:** All P2P RPC payloads are capped at `maxPayloadSize = 1 MiB` (1048576 bytes). Payloads exceeding this limit are rejected with `EFBIG` to prevent memory exhaustion.
- **Suspect Marking:** Peers are marked SUSPECT based on target ack timeout (not helper contact success), ensuring accurate failure detection.

### 8. Storage Backend Interface Contract
Any new `BlobStore` implementation MUST satisfy the following:
- **Interface:** Implement `PutBlob(hash, io.Reader)`, `GetBlob(hash) (io.ReadCloser, error)`, `DeleteBlob(hash)`, and `Close()`.
- **POSIX Errors (Rule 10):** All errors must map to `syscall` POSIX constants (e.g., `ENOENT` for missing blobs, `EIO` for I/O failures, `ECONNREFUSED` for network errors).
- **Panic Recovery (Rule 37):** The CASStore wrapper provides panic recovery; backends should not swallow panics.
- **DeleteBlob Idempotency:** Deleting a non-existent blob must return `nil` (not an error).
- **Content-Addressed (Rule 12):** Object key = content hash. No name-based storage.
- **Zero Dependencies (Rule 1):** Backends must use only Go stdlib (no external SDKs). The S3 backend uses a minimal SigV4 client (~200 lines of stdlib code).

---

## See Also

- [AI_FLYING_SOLO.md](AI_FLYING_SOLO.md) — Autonomous bug-fix workflow rules 51-63 (PR workflow, clean rebase, stale reviewer re-trigger, post-merge branch cleanup)
- [CONTRIBUTING.md](CONTRIBUTING.md) — Contribution guidelines and PR process
- [`openspec/config.yaml`](../openspec/config.yaml) — Single source of truth for all steering rules (Rule 39)
