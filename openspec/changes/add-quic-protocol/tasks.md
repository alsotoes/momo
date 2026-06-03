## 1. Foundation
- [ ] 1.1 Add the `github.com/quic-go/quic-go` dependency.
- [ ] 1.2 Define the `Transport` interface in `src/common` to unify `net.Conn` and `quic.Stream`.
- [ ] 1.3 Define the `ProtocolHandler` interface to encapsulate handshake and data framing logic.

## 2. Configuration & Factory
- [ ] 2.1 Update `src/common/config.go` to parse and validate the composite `protocol` field.
    - [ ] 2.1.1 Implement the logic to log a warning and fallback to `momo-tcp` if missing.
    - [ ] 2.1.2 Implement critical failure logic for unknown protocols.
- [ ] 2.2 Implement the `ProtocolFactory` in `src/common` to instantiate requested stacks.

## 3. Implementation
- [ ] 3.1 Refactor current Momo TCP logic into a `MomoProtocolHandler` using the `TCPTransport`.
- [ ] 3.2 Implement `MomoProtocolHandler` over `QUICTransport`.
- [ ] 3.3 Create a stub for `S3ProtocolHandler` as per Issue #133.
- [ ] 3.4 Upgrade the `Daemon` to start listeners for both TCP and QUIC concurrently if configured.

## 4. Verification
- [ ] 4.1 Unit tests for the `ProtocolFactory` selector.
- [ ] 4.2 Integration tests for `momo-quic` file transfers.
- [ ] 4.3 E2E benchmark comparing `momo-tcp` and `momo-quic` over high-latency links.
