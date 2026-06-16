# Contributing to Momo

Momo is developed with a focus on high-performance, security, and architectural cleanliness. As a project with heavy AI-agent involvement, we follow a strict development and review lifecycle.

## Development Workflow

1.  **Spec First**: All significant changes must start with an **OpenSpec** proposal in `openspec/changes/`.
2.  **Issue Linkage**: Every spec must be linked to a mirrored GitHub Issue to maintain traceability.
3.  **Feature Branching**: Work is performed in dedicated branches named `feature/<issue-number>-<description>`.
4.  **⚡ Bolt & 🛡️ Sentinel**: Code must adhere to the performance (Bolt) and security (Sentinel) patterns defined in the `.jules/` directory.

## Automated Code Review & Merge

Since this project leverages AI collaboration, every Pull Request is automatically reviewed by the **Gemini AI Reviewer**.

### The AI Reviewer ensures:
- **Zero-Crash Pattern**: No missing `recover()` blocks in goroutines and no unbounded readers.
- **POSIX Error Mapping**: Application errors must be matched with standard `syscall` constants.
- **Performance Integrity**: No regression in hot-path allocations.
- **Security Audit**: Detection of path traversal or injection risks.

### Autonomous Merge (All-Green Rule)
Momo utilizes an autonomous merge protocol. A PR is considered "Merge Ready" only when:
1.  The Gemini AI Reviewer provides a `✅` approval.
2.  ALL CI pipeline validations (Build, Test, Race, Goleak, etc.) are green.

Once these conditions are met, the AI Reviewer is authorized to perform an **automated merge** into the `master` branch.

### AI-to-AI Loop & Circuit Breaker
Automated maintenance agents (e.g., **@google-labs-jules**) can automatically fix issues identified by the Reviewer. To prevent infinite loops, a **3-push circuit breaker** is enforced. If an agent fails to resolve all issues within 3 attempts, the loop is locked, and manual intervention by **@alsotoes** is required.

### Autonomous Traceability
To satisfy **Steering Rule #11**, the Gemini AI Reviewer is authorized to autonomously create and link tracking issues to any Pull Request that lacks them. These issues are prefixed with `[Auto-Trace]` and use the `Resolves` keyword for formal linkage.

## CI/CD Pipeline

Every Pull Request must pass the full suite of validations before merging:
- **Unit & Fuzz Tests**: All tests in `src/` must pass with `-race` enabled.
- **Benchmark Gate**: Geomean performance must not degrade by more than 5%.
- **Smoke Tests**: Physical file replication verified across 5 suites (**TCP, QUIC, S3-TCP, S3-QUIC, and Scale/CAS**).
- **Version Consistency**: Go versions must be synchronized across all config files.

---
*Momo is a collaborative effort between human developers and AI agents (Gemini CLI, @google-labs-jules).*
