## 1. Implementation
- [ ] 1.1 Add the `github.com/quic-go/quic-go` dependency to the project workspace.
- [ ] 1.2 Implement a QUIC listener component within `src/server` that binds to a UDP port alongside the existing TCP daemon.
- [ ] 1.3 Refactor the client's `Connect` function in `src/common` to determine whether to dial via TCP or QUIC based on the active `ReplicationPrimarySplay` mode.
- [ ] 1.4 Maintain Momo's strict 19/32/64-byte metadata padding standard on QUIC streams identically to the TCP implementation.
- [ ] 1.5 Add table-driven unit tests validating `QUICDialer` and `QUICListener` abstractions without side-effects.
- [ ] 1.6 Assert concurrency safety on the UDP handlers using `defer goleak.VerifyNone(t)`.
- [ ] 1.7 Add E2E tests validating that switching to QUIC and back to TCP correctly hot-swaps payloads without restarting any daemon.
