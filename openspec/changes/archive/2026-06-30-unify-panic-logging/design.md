## Context

Momo implements robust crash-safety, but several `defer recover()` blocks across `momo_tcp.go`, `momo_quic.go`, and `crush.go` are silent (only assigning to `err` but omitting logging). **Project Steering Rule 9** mandates that all failures must be logged explicitly. To align with this, we must update all remaining silent recovery blocks to employ our established two-line recovery pattern (log + error).

## Goals / Non-Goals

**Goals:**
- Update all 18 silent recovery blocks to print detailed critical messages using `log.Printf`.
- Guarantee that all panic recoveries remain crash-safe and correctly propagate POSIX errors wrapping `syscall.EIO` or `syscall.EBADMSG`.

**Non-Goals:**
- Alter any functional data flow or network framing layouts.
- Change the structure or execution of background daemon thread pools.

## Decisions

### 1. Two-Line Recovery Refactoring
- **Choice:** Consistently update each target method's recovery block to include a distinct, trackable `log.Printf("CRITICAL: Recovered from panic in <MethodName>: %v", r)` statement.
- **Rationale:** This establishes perfect code consistency and ensures that any caught panic immediately emits a highly visible diagnostic trace in stderr, completely resolving logging gaps.

### 2. Standardizing Imports
- **Choice:** Ensure the standard `"log"` package is imported in `crush.go` (since it is not currently imported there) and verify it is correctly utilized.
- **Rationale:** Conforms to standard compilation requirements.

## Risks / Trade-offs

- **[Risk] Missing a Block:** Accidentally leaving some recovery blocks silent.
  - **Mitigation:** Rely strictly on our global `recover()` grep audit results containing all 53 recovery blocks across the codebase to perform exhaustive, sequential edits.
