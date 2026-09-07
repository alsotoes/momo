# 0001-add-adaptive-auth-backoff

## Status
Accepted

## Confidence
High

## Context
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

## Decision
- Adaptive Per-Source Backoff: The authentication limiter SHALL track consecutive failed authentication attempts per source address and reject further attempts from that source for a growing delay computed as `min(baseDelay * factor^failures, maxDelay)`. Successful authentication SHALL reset the source's state.
- Temporary Lockout: The authentication limiter SHALL impose a temporary lockout for a configurable duration once a source exceeds a maximum consecutive-failure threshold. During lockout, all authentication attempts from that source SHALL be rejected.
- Configurable Enablement: The limiter SHALL be disabled by default and only active when a positive backoff base delay is configured, so existing deployments and tests observe no behavioral change unless explicitly enabled.
- Bounded Memory: The limiter SHALL evict idle source entries after an idle window so that the number of tracked sources does not grow unboundedly (Rule 4 / Rule 32).
- Protocol Stability and Parity: The limiter SHALL NOT alter the wire handshake layout (Rules 7, 38) and SHALL apply across the challenge-response handshake paths used by `momo-tcp` and `momo-quic` data connections and the change-replication control channel (Rule 33).
- Concurrency Safety: The limiter SHALL be safe for concurrent access from many connection-handler goroutines.

## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: docs/blog/posts/014-confidential-dedup-oprf.md

## References
- Issue: #821
- PR: #819
- Spec: `openspec/changes/add-adaptive-auth-backoff/`
- Blog: docs/blog/posts/014-confidential-dedup-oprf.md

