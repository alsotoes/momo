# 0001-add-adaptive-auth-backoff

## Status
Accepted

## Confidence
High

## Context


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
- **Blog post**: docs/blog/posts/...md

## References
- Issue: #...
- PR: #...
- Spec: openspec/changes/add-adaptive-auth-backoff/
- Blog: docs/blog/posts/...md
