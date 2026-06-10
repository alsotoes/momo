# Contributing to Momo

Momo is developed with a focus on high-performance, security, and architectural cleanliness. As a project with heavy AI-agent involvement, we follow a strict development and review lifecycle.

## Development Workflow

1.  **Spec First**: All significant changes must start with an **OpenSpec** proposal in `openspec/changes/`.
2.  **Issue Linkage**: Every spec must be linked to a mirrored GitHub Issue to maintain traceability.
3.  **Feature Branching**: Work is performed in dedicated branches named `feature/<issue-number>-<description>`.
4.  **⚡ Bolt & 🛡️ Sentinel**: Code must adhere to the performance (Bolt) and security (Sentinel) patterns defined in the `.jules/` directory.

## Automated Code Review

Since this project leverages AI collaboration, every Pull Request is automatically reviewed by the **Gemini AI Reviewer**.

### The AI Reviewer ensures:
- **Zero-Crash Pattern**: No missing `recover()` blocks in goroutines and no unbounded readers.
- **POSIX Error Mapping**: Application errors must be matched with standard `syscall` constants.
- **Performance Integrity**: No regression in hot-path allocations.
- **Security Audit**: Detection of path traversal or injection risks.

## CI/CD Pipeline

Every Pull Request must pass the full suite of validations before merging:
- **Unit & Fuzz Tests**: All tests in `src/` must pass with `-race` enabled.
- **Benchmark Gate**: Geomean performance must not degrade by more than 5%.
- **Smoke Tests**: Physical file replication verified across 4 suites (TCP, QUIC, S3-TCP, S3-QUIC).
- **Version Consistency**: Go versions must be synchronized across all config files.

---
*Momo is a collaborative effort between human developers and AI agents (Gemini CLI, Jules).*
