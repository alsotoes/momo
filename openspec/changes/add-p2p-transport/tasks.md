## 1. Implementation
- [x] 1.1 Create the `src/p2p` directory and define the `Transport` and `Peer` interfaces.
- [x] 1.2 Implement the `TCPTransport` struct, including connection handling and peer management.
- [x] 1.3 Refactor `server/server.go` to use the new `p2p.Transport` for all network operations.
- [x] 1.4 Adapt the existing replication strategies to use the new peer map instead of static connections.
- [x] 1.5 Write unit tests for the `p2p` package.
- [x] 1.6 Run the performance and resilience measurement plan as defined in the proposal.
