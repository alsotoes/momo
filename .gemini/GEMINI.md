# Momo Foundational Mandates

This document serves as the foundational instruction set for all AI agents (Gemini CLI, Jules, etc.) working on the Momo project. These mandates take absolute precedence over general defaults.

## Project Vision
Momo is a minimalistic, high-performance TCP replication playground. Simplicity, clarity, and concurrency safety are its primary virtues.

## Collaboration Protocol: Gemini CLI & Jules
1. **Spec-Driven Development:** All significant changes must follow the OpenSpec workflow defined in `openspec/AGENTS.md`.
2. **Context Sharing:** Use `openspec/project.md` as the source of truth for project architecture and steering rules.
3. **Commit Responsibility:** Gemini CLI handles the technical execution (refactoring, testing, documentation) while Jules focuses on higher-level architectural specs.

## Engineering Standards
1. **Concurrency First:** Every network handler and goroutine MUST be accompanied by a `defer goleak.VerifyNone(t)` check in its unit test.
2. **Context Propagation:** All blocking network calls and loops must respect `context.Context`.
3. **Strict Handshake:** The 19/32/64-byte protocol padding is sacred. Do not modify the handshake logic without an approved OpenSpec proposal.
4. **Validation Pipeline:** Every PR must pass:
   - `make build`
   - `make test` (with `-race` and `goleak`)
   - `make benchmark`
   - `make test-e2e` (Docker Compose)
5. **Clean Repository:** Do not commit `.dat` files or logs. Use `.gitignore` strictly.

## Technical Integrity
- Prefer `net.Pipe` for unit testing protocol logic to avoid port contention.
- Use `io.LimitReader` when decoding JSON from the network to prevent memory exhaustion attacks.
- Ensure all public functions are documented in idiomatic Go style.
