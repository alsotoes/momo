# Adaptive Failed-Auth Backoff & Temporary Lockout

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/821

## A: AuthLimiter core (`src/common`)

- [x] A1. Add `AuthLimiter` type in a new `src/common/auth_limiter.go`:
  - per-source entry: `failures`, `nextAllow`, `lockoutUntil`, `lastSeen`
  - methods: `RecordFailure(source)`, `RecordSuccess(source)`, `Allow(source)`,
    `Reset()` (test hook), internal `evictIdle(now)`.
  - exponential curve `min(base*factor^fail, max)` and lockout gating.
- [x] A2. Config: add `AuthBackoffDelay int` (ms, `0`=disabled) to
  `ConfigurationGlobal` (`src/common/struct.go`) and parse in
  `loadGlobalConfig` (`src/common/config.go`) under `[global] auth_backoff_delay`.
- [x] A3. Defaults: `baseDelay=AuthBackoffDelay ms`, `factor=2`, `maxDelay=8s`
  (scaled), `maxFailures=5`, `lockoutDuration` scaled from base, eviction idle
  window defaults.
- [x] A4. Unit tests `src/common/auth_limiter_test.go`:
  - backoff monotonic growth + cap,
  - reset on success,
  - lockout threshold + expiry,
  - eviction of idle entries,
  - concurrency `-race` + `goleak.VerifyNone`.

## B: Integration into server handshakes

- [x] B1. `src/server/server.go`: in the connection-handler around
  `comm.HandshakeServer`, before crypto work check `Allow(remoteAddr)`; on
  early rejection close and return; record failure on auth error; clear on
  success.
- [x] B2. `src/server/replication.go`: same gating in the change-replication
  handler.
- [x] B3. Only engage when `cfg.Global.AuthBackoffDelay > 0`.

## C: Tests, Docs, Verification

- [x] C1. Integration test exercising the threshold then lockout via a real
  `net.Pipe`/`127.0.0.1:0` handshake with the limiter enabled (Rule 40).
- [x] C2. Docs parity (Rule 27): update `docs/CONFIGURATION.md` with
  `auth_backoff_delay`; note throttling in `docs/PROTOCOL.md`/security notes.
- [x] C3. `go build ./...`, `go test -race ./...`, `go vet ./...`, `gofmt`.
- [x] C4. Confirm `auth_backoff_delay=0` leaves all existing handshake tests
  unchanged (backward compatibility).

## Steering-Rule Compliance Notes

- **Rules 4/32:** bounded per-source map with idle eviction.
- **Rules 5/40:** tests + `goleak`/`-race`; ephemeral ports.
- **Rules 7/33/38:** wire protocol unchanged; applies across handshake paths.
- **Rule 27:** config doc parity.
