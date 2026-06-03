## 1. Implementation
- [ ] 1.1 Add the `github.com/quic-go/quic-go` dependency to the project workspace.
- [ ] 1.2 Implement a **Transport-Agnostic Connection Interface** in `src/common` to unify `net.Conn` and `quic.Stream`.
- [ ] 1.3 Implement a global QUIC listener in `src/server` that binds to a UDP port on all server nodes, capable of handling all replication modes.
- [ ] 1.4 Refactor `Connect`, `SendFile`, and `getFile` to utilize the new transport abstraction, allowing any mode to run over QUIC.
- [ ] 1.5 Update the client and internal peer dialers to support a "QUIC-Preferred" mode with transparent TCP fallback.
- [ ] 1.6 Maintain Momo's strict 19/32/64-byte metadata padding standard on QUIC streams identically to the TCP implementation.
- [ ] 1.7 Add table-driven unit tests validating the unified transport layer and dual-protocol handshake logic.
- [ ] 1.8 Add E2E tests validating that `Chain` and `Splay` modes work correctly over QUIC between distributed server nodes.
- [ ] 1.9 Assert concurrency safety and resource management across both TCP and UDP/QUIC handlers.
