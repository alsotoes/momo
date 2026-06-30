## Context

With Momo supporting four distinct wire protocols (`momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`), there is a risk that functional features (such as object listing, deletion, deduplication, and error mappings) could diverge between transport implementations. **Project Steering Rule 33** mandates perfect protocol feature parity. To enforce this, we must design an automated validation framework that executes all core operations and asserts identical outcomes across all protocols.

## Goals / Non-Goals

**Goals:**
- Implement a unified matrix integration test running across TCP and QUIC, under both native and S3 configurations.
- Assert perfect semantic parity for Put, Get, Delete, and List operations.
- Enforce standard POSIX error code mappings across all four protocols on equal failures (such as `syscall.ENOENT` or `syscall.EBADMSG`).

**Non-Goals:**
- Modify low-level wire frames or application protocols.
- Introduce new protocols beyond the four already supported.

## Decisions

### 1. Unified Parity Test Suite
- **Choice:** Create a dedicated, unified matrix integration test suite inside the `transport` or `server` test modules.
- **Rationale:** Instead of copying test logic across package boundaries, we will write a generic table-driven test that accepts a protocol type, spins up ephemeral daemons/listeners, performs the complete suite of file actions, and validates the DB state. This guarantees any future protocol addition or optimization automatically inherits full parity checks.

### 2. Ephemeral Ports and Concurrency
- **Choice:** Allocate random ephemeral ports (`127.0.0.1:0`) and run parallelized tests with strict goleak audits.
- **Rationale:** Hard-coded testing ports lead to socket conflicts and intermittent CI pipeline failures. Utilizing ephemeral sockets with short deadlines prevents port collision and keeps pipeline execution extremely stable.

## Risks / Trade-offs

- **[Risk] High CI Execution Time:** Running four full-stack daemon setups can slow down test loops.
  - **Mitigation:** Use ultra-lightweight 10-byte payloads for validation and pre-initialize temporary Bbolt database directories to keep overall runtimes below 500 milliseconds.
