# Proposal: Adaptive Failed-Auth Backoff & Temporary Lockout

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/821

- **Champion:** opencode (deepseek-v4-flash-free)
- **Status:** `Draft`

## 1. Problem

Momo authenticates every connection via a challenge-response HMAC handshake
(`src/common/auth.go`). On failure, `ChallengeResponseServerPeer` returns an
`EACCES` error and the connection handler simply closes the socket. There is
**no throttling**: an attacker (or a misbehaving peer) that knows they will fail
authentication can hammer the listener with an arbitrary number of handshake
attempts per second. Each attempt costs the server:

- two `crypto/rand` reads,
- an HMAC-SHA256 verification, and
- a connection-handler goroutine with associated logging.

Because the auth token is fixed and shared cluster-wide, an online brute-force
against the HMAC response space, or more realistically a forged/short-token
probe, is currently un-metered. This is an online brute-force and resource
abuse (slow, but unbounded probe-rate) exposure.

## 2. Proposed Solution

Introduce a stateful, per-source **auth limiter** that applies adaptive
exponential backoff on consecutive failures and a temporary lockout after a
configurable threshold, reset on successful authentication.

### 2.1 `AuthLimiter` (new, in `src/common`)

A mutex-guarded, source-keyed registry. It tracks, per source address:

- consecutive failure count,
- the `nextAllow` time computed from an exponential backoff, and
- a `lockoutUntil` time after a threshold is exceeded.

Key behaviors:

1. **Adaptive backoff**: after each consecutive failure, the per-source delay
   grows so that further attempts from that source are rejected until
   `baseDelay * factor^failures` (capped at `maxDelay`). This creates an
   exponential backoff curve.
2. **Temporary lockout**: once a source reaches `maxFailures` consecutive
   failures, it enters a lockout for `lockoutDuration`; during lockout all
   attempts are rejected regardless of elapsed backoff.
3. **Reset on success**: a successful auth clears the source state, so a
   legitimate client that eventually succeeds (e.g. flips a bad transient token)
   is released immediately.
4. **Bounded memory**: entries are cleaned up after an idle window to prevent
   unbounded growth from a large source space (Rule 4/32).

### 2.2 Config knob

Add an `AuthBackoffDelay` (in milliseconds) under `[global]`. `0` (default)
disables throttling so existing behavior is unchanged unless explicitly
enabled — preserving backward compatibility for tests and simple deployments.

### 2.3 Integration point

The limiter wraps the challenge-response call in the connection-handler paths:

- `src/server/server.go` (primary data handshake, `HandshakeServer`),
- `src/server/replication.go` (change-replication control channel).

On auth failure the handler informs the limiter of the failing source; on
success it clears it. When the limiter reports a lockout/backoff for a source,
the handler rejects the connection early **before** doing crypto work.

## 3. Wire & Protocol Impact

None. `auth.go` adds no bytes to the handshake; the wire layout, the nonce and
response sizes, and the mode-byte parsing are untouched (Rules 7, 38). The
limiter only changes *server-side acceptance policy*.

## 4. Testing

- Unit tests for the backoff curve (monotonic growth, cap, reset on success).
- Unit tests for lockout threshold and lockout expiry.
- Idle-eviction test to assert bounded memory (Rule 32).
- Integration test: `N` failed attempts from one source then a lockout, and a
  successful attempt that clears state.
- Concurrency-safety: `goleak.VerifyNone` + `-race` on the limiter (Rule 5).

## 5. Backward Compatibility

Enabled only when `auth_backoff_delay > 0`. Default `0` = disabled → all
existing tests and behavior unchanged. No load-path changes. Compliance with
Rules 4 (bounded memory), 5 (tests + concurrency), 7 (wire stability), 33
(applies across `momo-tcp`/`momo-quic` handshakes), 38 (disjoint namespaces).
