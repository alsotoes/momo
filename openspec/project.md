# Project Context

## Purpose
Momo is a high-performance, distributed **Object Storage system** written in Go. It demonstrates several replication strategies (None, Chain, Splay, Primary-Splay) and a simple, metrics‑driven controller that can dynamically switch strategies at runtime (a "polymorphic" system).

## Tech Stack
- Go (1.20+)
- Configuration: `gopkg.in/ini.v1`
- System Metrics: `github.com/shirou/gopsutil/v3`
- Testing: Go standard `testing` package, `go.uber.org/goleak` for concurrency safety.
- E2E & Containerization: Docker, Docker Compose
- CI/CD: GitHub Actions (build, test, benchmark, smoke test, auto reviewer)

## Project Conventions

### Code Style
- Idiomatic Go conventions.
- Use `context.Context` for managing graceful shutdown and timeouts across network sockets and goroutines.
- Use `go fmt`, `go vet`, and standard linters. All code must pass `make test` before pushing.
- Avoid global state mutations outside of initialization unless strictly synchronized.

### Architecture Patterns
- **TCP & QUIC Daemons:** Nodes act as independent Daemons assigned an ID (0, 1, 2). Node 0 is typically the primary and metrics authority. Daemons listen on both standard TCP and UDP (QUIC) ports simultaneously.
- **Polymorphic System:** The system monitors CPU/Memory and dynamically adjusts the replication tier across the cluster by broadcasting JSON payloads via a dedicated TCP port.
- **Replication Strategies:**
  - `ReplicationNone` (4): No replication. (TCP)
  - `ReplicationChain` (1): Node 0 -> Node 1 -> Node 2. (TCP for high-bandwidth LAN)
  - `ReplicationSplay` (2): Node 0 -> Node 1 and Node 2. (TCP for high-bandwidth LAN)
  - `ReplicationPrimarySplay` (3): Client -> Node 0, Node 1, Node 2 concurrently. (QUIC for lossy/WAN networks to prevent Head-of-Line blocking and provide 0-RTT/Connection Migration).

### Testing Strategy
- **Unit Testing:** Table-driven tests heavily preferred. Functions must be isolated, using mocks (like `net.Pipe` or dummy TCP listeners) for network boundaries.
- **Concurrency Safety:** ALL tests spawning network handlers or goroutines must use `defer goleak.VerifyNone(t)` and `go test -race`.
- **Load Testing:** `Benchmark*` functions are required to stress-test primary daemon endpoints under high concurrent file uploads.
- **End-to-End (E2E):** Docker Compose scripts (`.github/scripts/test-e2e.sh`) boot up isolated networks to physically verify byte chunks successfully transfer to sibling volumes.

### Git Workflow
- **Branch-Based Development:** Each new specification implementation MUST be developed in a dedicated feature branch.
- **Pull Requests:** Upon completion of a feature or fix, a Pull Request (PR) to the `master` branch must be created for review and validation. Direct commits to `master` are only permitted for trivial documentation fixes or small configuration adjustments.
- `auto_reviewer.yml` auto-assigns the lead maintainer (`alsotoes`).
- CI/CD verifies Unit, Race, Benchmark, E2E, and Code Coverage before merging.

## Domain Context
- **Protocol:** Handshakes start with a fixed 19-byte timestamp, followed by a replication mode code, and then file metadata (MD5, padded filename, padded filesize). The payload streams in 1024-byte chunks.
- **Timestamps:** To prevent race conditions in replication shifts, nodes use Unix Nanoseconds to evaluate if a client request's timestamp supersedes an old replication setting.

## Important Constraints
- **Security:** There is no authentication or TLS currently built into Momo. It operates entirely over plain TCP.
- **Error Handling:** Daemons invoke `os.Exit(1)` on fatal boot issues (e.g. port taken), but client connection handling failures must simply terminate the local goroutine, closing the `net.Conn`.
- **Metadata Limits:** File metadata relies on strictly padded string limits (e.g., 64 bytes for names).

## External Dependencies
- `gopsutil` requires native host OS file structures (like `/proc`) for memory/CPU reading, heavily factoring into how the `metrics` package is tested (which avoids pure mocking for system reads when possible).

## Project Steering Rules
When analyzing or suggesting code generation, agents must adhere to the following steering rules:
1. **Preserve Simplicity:** Momo is a minimalistic playground. Avoid over-engineering or introducing heavy external frameworks (like gRPC or complex ORMs) unless explicitly requested.
2. **Decentralized Primary:** Momo utilizes a **Balanced Primary** model enabled by the CRUSH-lite algorithm. Any node in the cluster can act as the primary for a specific object based on its content hash. Do not build logic that assumes a single fixed coordinator or entry point.
3. **Branching Mandate:** All significant features or spec-driven changes MUST originate from a feature branch and be merged via a validated PR to the `master` branch.
4. **Zero-Crash Pattern:** (Mandatory) All code must follow defensive stability standards. Never assume external data is well-formed. Every goroutine MUST implement panic recovery. Use bounded readers and fixed-size buffers to prevent resource exhaustion.
5. **Rigorous Validation:** ALL functional additions MUST include corresponding unit/integration tests. You must assert concurrency safety (`goleak.VerifyNone(t)`) and context cancellations when modifying network layers.
6. **Dynamic Hot-Swapping:** A core feature of Momo is the ability to change replication schemes *on the fly* by communicating with the daemons. The daemons must never require a restart to adopt a new replication mode. This is achieved by timestamping configuration changes: existing goroutines/connections finish their work using the "Old" configuration, while new client connections or internal threads adopt the "New" configuration based on the latest global timestamp. Always preserve and test this dynamic behavioral shifting.
7. **Protocol Stability:** The TCP handshaking metadata relies entirely on strict byte boundaries (`19`, `32`, `64` bytes padding). Do not alter or break these payload sizes without extensive refactoring and migration planning.
8. **Go Version Consistency:** All configuration files defining the Go language version (e.g., `go.mod`, GitHub Actions `setup-go` steps, `Dockerfile`, `dev.nix`, etc.) MUST be strictly synchronized. Any spec or code change that bumps the Go version must validate and update every occurrence across the entire repository to prevent build fragmentation or drift.
9. **No Silent Failures:** Return explicit errors for network failures. Do not swallow errors in goroutines; use `log.Printf` to emit failures for traceability.
10. **POSIX Error Mapping:** All application-level errors (e.g., authentication failures, hash mismatches) MUST be mapped to standard `syscall` POSIX constants (e.g., `syscall.EACCES`, `syscall.EBADMSG`) to ensure consistent, standard error propagation across the cluster. This follows the standardized pattern established in [PR #97](https://github.com/alsotoes/momo/pull/97).
11. **Issue-Spec Traceability:** (Mandatory) ALL project specifications (`openspec/`) MUST be mirrored as GitHub Issues. Every spec file must explicitly link to its corresponding GitHub Issue URL, and the GitHub Issue must link back to the spec file in the repository. This ensures synchronization parity and end-to-end traceability for all feature designs and architectural shifts.
12. **Object Storage Paradigm:** Momo is a distributed Object Storage system. All storage operations MUST be content-addressable and use algorithmic placement (specifically a Go implementation of **Sage Weil's CRUSH algorithm**) to ensure perfect load balancing and infinite scalability without central registry bottlenecks.
13. **PR Success Criteria (All-Green Rule):** A Pull Request is only considered "Merge Ready" when:
    - The Gemini AI Reviewer provides a `✅` approval.
    - ALL GitHub Action status checks (Build, Test, Race, Goleak, Lint) are green.
    - Once satisfied, the AI Reviewer is authorized to perform an automated merge.
14. **AI-to-AI Collaboration & Loop Prevention:** Automated maintenance agents (e.g., Jules) can automatically fix issues identified by the AI Reviewer. To prevent infinite loops, a **3-push circuit breaker** is enforced. If an agent fails to resolve all issues within 3 attempts, manual intervention by @alsotoes is mandatory.
15. **Token Efficiency & Filtering:** ALL AI-driven audits (Reviewer, Jules, etc.) MUST filter out non-essential files (`vendor/`, `go.sum`, binary blobs) from their context. Diffs larger than **1,000 lines** MUST be truncated with a warning to prevent token exhaustion.
16. **Human-in-the-Loop Trigger:** Destructive operations (e.g., `git push --force`, `rm -rf` in production directories, or `gh pr close`) MUST NEVER be initiated by the AI loop without an explicit instruction from **@alsotoes** or a 3-attempt failure signal.
17. **Atomic AI Tasks:** Automated agents (Jules) MUST work on **one specific issue at a time**. Multi-issue "batch fixes" are prohibited to prevent large, unmanageable diffs and overlapping logical conflicts.
18. **Circuit Breaker Persistence:** The **3-push circuit breaker** (Rule 14) applies across all AI entities. If a collective AI effort (Reviewer + Jules) cannot reach an "All-Green" state in 3 iterations, the automated pipeline MUST lock the PR and tag the maintainer.
19. **Resource-Aware Hashing:** When validating data integrity, AI agents MUST prioritize **TeeReader** and stack-allocated buffers (Bolt Pattern) to ensure that automated security checks do not become performance bottlenecks.