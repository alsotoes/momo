# Project Context

## Purpose
Momo is a minimal TCP-based file replication playground written in Go. It demonstrates several replication strategies (None, Chain, Splay, Primary-Splay) and a simple, metrics‑driven controller that can dynamically switch strategies at runtime (a "polymorphic" system).

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
- **TCP Daemons:** Nodes act as independent Daemons assigned an ID (0, 1, 2). Node 0 is typically the primary and metrics authority.
- **Polymorphic System:** The system monitors CPU/Memory and dynamically adjusts the replication tier across the cluster by broadcasting JSON payloads via a dedicated TCP port.
- **Replication Strategies:**
  - `ReplicationNone` (4): No replication.
  - `ReplicationChain` (1): Node 0 -> Node 1 -> Node 2.
  - `ReplicationSplay` (2): Node 0 -> Node 1 and Node 2.
  - `ReplicationPrimarySplay` (3): Client -> Node 0, Node 1, Node 2 concurrently.

### Testing Strategy
- **Unit Testing:** Table-driven tests heavily preferred. Functions must be isolated, using mocks (like `net.Pipe` or dummy TCP listeners) for network boundaries.
- **Concurrency Safety:** ALL tests spawning network handlers or goroutines must use `defer goleak.VerifyNone(t)` and `go test -race`.
- **Load Testing:** `Benchmark*` functions are required to stress-test primary daemon endpoints under high concurrent file uploads.
- **End-to-End (E2E):** Docker Compose scripts (`.github/scripts/test-e2e.sh`) boot up isolated networks to physically verify byte chunks successfully transfer to sibling volumes.

### Git Workflow
- Direct commits to `master` allowed for minor changes, but PRs are preferred for robust testing.
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
2. **Respect Affinities:** Honor the hardcoded cluster assumptions (Daemon 0 = primary/metrics authority, Daemons 1/2 = followers/fallback). Do not build complex dynamic service discovery.
3. **Rigorous Validation:** ALL functional additions MUST include corresponding unit/integration tests. You must assert concurrency safety (`goleak.VerifyNone(t)`) and context cancellations when modifying network layers.
4. **Dynamic Hot-Swapping:** A core feature of Momo is the ability to change replication schemes *on the fly* by communicating with the daemons. The daemons must never require a restart to adopt a new replication mode. This is achieved by timestamping configuration changes: existing goroutines/connections finish their work using the "Old" configuration, while new client connections or internal threads adopt the "New" configuration based on the latest global timestamp. Always preserve and test this dynamic behavioral shifting.
5. **Protocol Stability:** The TCP handshaking metadata relies entirely on strict byte boundaries (`19`, `32`, `64` bytes padding). Do not alter or break these payload sizes without extensive refactoring and migration planning.
6. **No Silent Failures:** Return explicit errors for network failures. Do not swallow errors in goroutines; use `log.Printf` to emit failures for traceability.