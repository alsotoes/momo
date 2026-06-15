# Momo Foundational Mandates

This document serves as the foundational instruction set for all AI agents (Gemini CLI, Jules, etc.) working on the Momo project. These mandates take absolute precedence over general defaults.

## Project Vision
Momo is a high-performance, distributed Object Storage system written in Go. Simplicity, clarity, and concurrency safety are its primary virtues.

## Collaboration Protocol: Gemini CLI & Jules
1. **Spec-Driven Development:** All significant changes must follow the OpenSpec workflow defined in `openspec/AGENTS.md`.
2. **Context Sharing:** Use `openspec/project.md` as the source of truth for project architecture and steering rules.
3. **Decentralized Execution:** Momo is a **Balanced Primary** cluster. Any node can initiate transactions. Gemini CLI handles the technical execution (refactoring, testing, documentation) while Jules focuses on higher-level architectural specs.
4. **Branching Strategy:** Each new specification implementation MUST be developed in a dedicated feature branch. Upon completion, a Pull Request (PR) to the `master` branch must be created for review and validation.

## Engineering Standards
1. **Concurrency First:** Every network handler and goroutine MUST be accompanied by a `defer goleak.VerifyNone(t)` check in its unit test.
2. **Context Propagation:** All blocking network calls and loops must respect `context.Context`.
3. **Strict Handshake:** The 19/32/64-byte protocol padding is sacred. Do not modify the handshake logic without an approved OpenSpec proposal.
4. **Zero-Crash Pattern:** (Mandatory) All code must adhere to defensive stability standards:
    - **Defensive Parsing:** Never assume input data (network, file, or config) is well-formed. Use `SafeParseInt` or bounded standard library functions. Validate character sets and ranges before processing.
    - **Panic Recovery:** Every background or dynamically spawned goroutine MUST include a `defer recover()` block to prevent a single failure from terminating the entire process.
    - **Bounded Resources:** Always use `io.LimitReader` and fixed-size buffers when reading from untrusted sources to prevent resource exhaustion (DoS).
5. **Validation Pipeline:** Every PR must pass:
   - `make build`
   - `make test` (with `-race` and `goleak`)
   - `make benchmark`
   - `make test-e2e` (Docker Compose)
6. **POSIX Error Mapping:** All application-level errors (e.g., authentication failures, hash mismatches) MUST be mapped to standard `syscall` POSIX constants (e.g., `syscall.EACCES`, `syscall.EBADMSG`) to ensure consistent, standard error propagation across the cluster. This follows the standardized pattern established in [PR #97](https://github.com/alsotoes/momo/pull/97).
7. **Issue-Spec Traceability:** (Mandatory) ALL project specifications (`openspec/`) MUST be mirrored as GitHub Issues. Every spec file must explicitly link to its corresponding GitHub Issue URL, and the GitHub Issue must link back to the spec file in the repository. This ensures synchronization parity and end-to-end traceability for all feature designs and architectural shifts.
8. **Object Storage Paradigm:** Momo is a distributed Object Storage system. All storage operations MUST be content-addressable and use algorithmic placement (specifically a Go implementation of **Sage Weil's CRUSH algorithm**) to ensure perfect load balancing and infinite scalability without central registry bottlenecks.
9. **AI Governance & Token Efficiency:** All AI-driven operations MUST prioritize token conservation. Diffs larger than 1,000 lines MUST be truncated. Automated agents MUST work on one specific issue at a time.
10. **Human-in-the-Loop:** Destructive operations (force-push, directory deletion) REQUIRE explicit approval from **@alsotoes**. The 3-push circuit breaker is mandatory for all AI-to-AI loops.
11. **Autonomous Traceability:** The AI Reviewer is authorized to autonomously create missing tracking issues (Rule 11) and link them to PRs to ensure consistent project organization.
5. **Clean Repository:** Do not commit `.dat` files or logs. Use `.gitignore` strictly.

## Technical Integrity
- Prefer `net.Pipe` for unit testing protocol logic to avoid port contention.
- Use `io.LimitReader` when decoding JSON from the network to prevent memory exhaustion attacks.
- Ensure all public functions are documented in idiomatic Go style.
