# Spec: Unified panic recovery

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1160

## Requirements

### PRN-A1: Shared recovery helpers

**Requirement:** `src/common` MUST expose `RecoverErr`, `RecoverErrWith`,
`RecoverErrClose`, and `RecoverClose`. Each MUST call `recover()` directly so it
works when used as the deferred function, MUST log the panic via `log.Printf`,
and MUST map the failure to a POSIX `syscall` constant (`syscall.EIO` by default).

**Scenario: a recovery helper maps a recovered panic to EIO**

Given a function with a named `error` return and `defer common.RecoverErr("op", &err)`,
When the function panics,
Then the returned error wraps `syscall.EIO`,
And the panic is logged once.

### PRN-A2: Helpers are zero-allocation

**Requirement:** The helpers MUST take `op` (and `errno`) by value so that using
them as a deferred function adds no heap allocation and no closure capture.

**Scenario: adopting a helper does not add per-call allocation**

Given a hot path that recovers panics,
When its inline recovery closure is replaced by a shared helper,
Then the deferred call performs no heap allocation.

### PRN-A3: Resource-releasing recoveries close the resource

**Requirement:** Recovery helpers that release a resource (`RecoverErrClose`,
`RecoverClose`) MUST close the supplied `io.Closer` after a recovered panic, so a
panicking connection or file cannot remain open (Rule 43).

**Scenario: a panicking connection handler closes its socket**

Given a connection handler with `defer common.RecoverClose("op", conn)`,
When the handler panics,
Then `conn` is closed.

### PRN-A4: Layers adopt the shared helpers

**Requirement:** The transport, storage, server, client, and common layers MUST
use the shared helpers for uniform recoveries (log + POSIX-mapped named return,
optionally closing a single resource). Recoveries requiring multi-resource
cleanup or per-object log detail MAY remain explicit and inline.

**Scenario: the uniform recovery pattern is centralized**

Given the transport read/write/handshake handlers and the CAS store methods,
When they recover from a panic,
Then they use `common.RecoverErr` / `common.RecoverErrClose` rather than an
inline closure.
