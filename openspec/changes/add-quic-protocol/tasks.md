## 1. Core Architecture (Issue #131)
- [ ] 1.1 Define the `Communicator` interface in `src/common` to unify `momo-tcp`, `momo-quic`, and `s3` communication logic.
- [ ] 1.2 Implement the `ProtocolFactory` selector.
- [ ] 1.3 Refactor `src/server/server.go` to be transport-agnostic, accepting a `Communicator` for incoming connections.
- [ ] 1.4 Refactor `src/common/replication.go` to utilize `Communicator` for all outbound peer transmissions.

## 2. Protocol Implementations
- [ ] 2.1 Implement `MomoTCPCommunicator` (preserving legacy behavior).
- [ ] 2.2 Implement `MomoQUICCommunicator` (integrating `quic-go`).
- [ ] 2.3 Implement `S3Communicator` stub (Issue #133).

## 3. Configuration & Validation
- [ ] 3.1 Update `src/common/config.go` for the composite `protocol` field.
    - [ ] 3.1.1 Log fallback warning if field is missing.
    - [ ] 3.1.2 implement fatal error on invalid protocol strings.

## 4. Verification
- [ ] 4.1 Unit tests for `Communicator` implementations.
- [ ] 4.2 Integration tests for protocol-agnostic replication (`Chain` mode over QUIC).
- [ ] 4.3 Benchmark comparison between `momo-tcp` and `momo-quic`.
