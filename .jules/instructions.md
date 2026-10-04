# Instructions for Google Labs Jules (@google-labs-jules)

## 1. Single Source of Truth for Steering Rules (Rule 39)
All project steering rules, engineering standards, architecture constraints, and quality gates are centrally defined in **one** primary file:
[`openspec/config.yaml`](../openspec/config.yaml) (under the `context` block, "Project Steering Rules").

**MANDATE**: You MUST read and follow `openspec/config.yaml` at the start of every task before generating a plan or modifying code. Do not rely on assumptions or incomplete context.

---

## 2. Core Directives for Jules

### A. Knowledge File Integrity (Rule 44)
- Files in `.jules/` (`bolt.md` and `sentinel.md`) are cumulative historical knowledge bases.
- **NEVER** overwrite, truncate, replace, or delete existing entries.
- You MUST **ONLY APPEND** new learning entries to the very end of `.jules/bolt.md` and `.jules/sentinel.md`.
- Always format dates accurately using the task/PR creation date (Rule 85).

### B. Defensive Stability & Zero-Crash Pattern (Rule 4 & Rule 37)
- Never assume external network data is well-formed.
- Every spawned goroutine MUST implement deferred panic recovery.
- Use the unified two-line panic recovery pattern: log the panic via `log.Printf` and wrap a standard POSIX `syscall` constant (e.g. `syscall.EIO`, `syscall.EBADMSG`).

### C. Concurrency Safety & Testing (Rule 5 & Rule 40)
- All network and goroutine tests MUST include `defer goleak.VerifyNone(t)`.
- All network tests MUST dynamically bind to ephemeral ports (`127.0.0.1:0`) and resolve with `l.Addr().String()` to avoid port collision.
- Run `make test` before opening or updating a Pull Request.

### D. Formatting & Vendoring Parity (Rule 25 & Rule 26)
- Always run `make fmt` (`go fmt ./...`) before committing code to prevent CI failures.
- If dependencies change, run `make vendor` (`go work vendor`) to maintain workspace parity.

### E. Spec-First & PR Traceability (Rule 11, Rule 68, Rule 73)
- Features and significant enhancements must link to an OpenSpec proposal and tracking issue.
- Pull Request descriptions MUST include the keyword `Resolves #ISSUE_ID`.
- When Jules creates a PR, the PR is automatically tracked under the `jules` label (Rule 68).
