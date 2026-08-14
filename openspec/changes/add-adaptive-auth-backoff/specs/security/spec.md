# Adaptive Failed-Auth Backoff & Temporary Lockout

## Purpose

This specification hardens Momo's challenge-response authentication against
online brute-force and resource-abuse by throttling failed handshake attempts
per source with adaptive exponential backoff and a temporary lockout, while
leaving the wire protocol byte-for-byte unchanged.

## ADDED Requirements

### Requirement: Adaptive Per-Source Backoff

The authentication limiter SHALL track consecutive failed authentication
attempts per source address and reject further attempts from that source for a
growing delay computed as `min(baseDelay * factor^failures, maxDelay)`.
Successful authentication SHALL reset the source's state.

#### Scenario: repeated failures produce an exponentially growing delay
- **GIVEN** a source `S` with zero prior failures and `baseDelay = D`
- **WHEN** `S` fails authentication three consecutive times
- **THEN** the delay following the third failure is strictly greater than the
  delay following the second failure, and each delay is capped at `maxDelay`.

#### Scenario: a successful authentication resets the backoff
- **GIVEN** a source `S` that has accumulated consecutive failures
- **WHEN** `S` then authenticates successfully
- **THEN** the source's failure count is reset to zero and its next allowed
  attempt is immediate, with no residual delay.

### Requirement: Temporary Lockout

The authentication limiter SHALL impose a temporary lockout for a configurable
duration once a source exceeds a maximum consecutive-failure threshold. During
lockout, all authentication attempts from that source SHALL be rejected.

#### Scenario: exceeding the failure threshold triggers a lockout
- **GIVEN** a per-source failure threshold `T` and a lockout duration `L`
- **WHEN** a source accumulates more than `T` consecutive failures
- **THEN** the source is denied authentication for `L` duration, after which
  the failure counter resets and the source may authenticate again.

#### Scenario: lockout rejects attempts that replay the backoff window
- **GIVEN** a source currently inside its lockout period
- **WHEN** the source attempts authentication
- **THEN** the attempt is rejected regardless of any elapsed backoff delay.

### Requirement: Configurable Enablement

The limiter SHALL be disabled by default and only active when a positive
backoff base delay is configured, so existing deployments and tests observe no
behavioral change unless explicitly enabled.

#### Scenario: disabled default preserves existing behavior
- **GIVEN** a configuration with no (or zero) `auth_backoff_delay`
- **WHEN** a client authenticates
- **THEN** authentication succeeds or fails with exactly the prior behavior,
  with no delay and no rejection attributable to throttling.

### Requirement: Bounded Memory

The limiter SHALL evict idle source entries after an idle window so that the
number of tracked sources does not grow unboundedly (Rule 4 / Rule 32).

#### Scenario: idle sources are evicted
- **GIVEN** a source `S` that failed once and then is idle beyond the eviction
  window
- **WHEN** `S` next attempts authentication
- **THEN** it is treated as a fresh source with zero prior failures.

### Requirement: Protocol Stability and Parity

The limiter SHALL NOT alter the wire handshake layout (Rules 7, 38) and SHALL
apply across the challenge-response handshake paths used by `momo-tcp` and
`momo-quic` data connections and the change-replication control channel (Rule
33).

#### Scenario: wire handshake bytes are unchanged
- **GIVEN** the limiter is enabled
- **WHEN** a client performs a valid challenge-response handshake
- **THEN** the bytes exchanged (nonce size, response size, mode parse) are
  identical to when the limiter is disabled.

### Requirement: Concurrency Safety

The limiter SHALL be safe for concurrent access from many connection-handler
goroutines.

#### Scenario: concurrent failures from many sources
- **GIVEN** many goroutines recording failures for distinct sources concurrently
- **WHEN** all records complete
- **THEN** no data race is observed under `-race`, and each source's state is
  internally consistent.
