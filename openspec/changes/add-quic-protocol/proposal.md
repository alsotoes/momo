# Change: Robust Multi-Protocol Transport Layer
**Related Issues:** #131, #132, #133

## Why
Momo currently intertwines its communication protocol with its core replication logic. To support a pluggable architecture, we must separate **how** nodes communicate (the Protocol) from **what** they do with the data (the Replication Mode). This decoupling allows us to implement `momo-tcp`, `momo-quic`, `s3-tcp`, and `s3-quic` as interchangeable transport plugins while ensuring the `Chain`, `Splay`, and `PrimarySplay` strategies function identically across all of them.

### Architectural Rationale: Layered Protocol Design
As per Issue #131, the system will move to a three-layer architecture:

#### 1. Communication Layer (Transport + App Protocol)
This layer handles the physical movement of bytes. It includes the carrier transport (TCP/UDP) and the application-level framing (Momo's 83-byte handshake or S3's REST API).
- **Momo-TCP**: Legacy transport.
- **Momo-QUIC**: Modern, secure-by-default transport.
- **S3 API**: Industry-standard interoperability.

#### 2. Core Replication Logic (Agnostic)
The core logic defines the data distribution path (e.g., `Chain` replication where Node 1 forwards to Node 2). This logic is **completely agnostic** of the communication layer. It receives a file and instructions on where to send it next, calling the `ProtocolFactory` to get a connection without knowing if it's TCP, QUIC, or S3.

#### 3. State Management (Polymorphic System)
Determines the current replication mode based on metrics, completely separated from the network stack.

## What Changes
- Integrate `github.com/quic-go/quic-go` for QUIC/H3 support.
- Implement a `ProtocolFactory` that provides a unified `Communicator` interface.
- Refactor `src/server/server.go` and `src/common/replication.go` to use this `Communicator` instead of direct `net.Conn` calls.
- Ensure the handshake for selecting replication modes (the "Mode Handshake") is a discrete step within the `Communicator` logic, allowing it to be reused or mapped to different protocol-specific handshakes (like S3 headers).
- Update `loadGlobalConfig` to parse `protocol=momo-quic` etc., from the `[global]` section.

## Impact
- Affected specs: `replication`, `security`
- Affected code: `src/common` (dialer/listener abstractions), `src/server` (Daemon logic), `go.mod`


