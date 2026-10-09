# Change: Unified panic-recovery helpers (Rules 37 & 43)

**Related Issues:**
- https://github.com/alsotoes/momo/issues/1160 (tracking)

## Why

Panic recovery (Rules 37 and 43) was implemented as ~90 hand-rolled
`defer func(){ if recover() ... }()` closures spread across `src/`. They
duplicated the same two-line pattern, drifted in message format, were
individually untestable, and in a few goroutine cases could repanic on a nil
dereference while handling the original panic.

## What Changes

- Add `src/common/recover.go` with four shared helpers:
  - `RecoverErr(op, &err)` — Rule 37: log + assign `syscall.EIO` to a named return.
  - `RecoverErrWith(op, errno, &err)` — Rule 37 with a custom POSIX errno.
  - `RecoverErrClose(op, &err, closer)` — Rule 37 + Rule 43 resource release.
  - `RecoverClose(op, closer)` — Rule 43 for goroutine closures with no named return.
- Adopt the helpers across the transport, storage, server, client, and common
  layers, deleting the duplicated inline closures.
- Keep genuinely custom recoveries (multi-resource cleanup, per-object log
  detail, nil-guarded receivers) explicit and inline.
- Document the standard in `docs/CORE/STANDARDS.md` (§9).
