# Change: Robust Multi-Protocol Transport Layer
**Related Issues:** #131, #132, #133

## Why
Momo currently intertwines its communication protocol with its core replication logic. To support a pluggable architecture (Issue #131), we must separate **how** nodes communicate from **what** they do with the data. This spec focuses on implementing the **Momo-QUIC** variant and the underlying **Protocol Factory**, while the **S3** variant is tracked separately in `openspec/changes/add-s3-protocol/`.

### Architectural Rationale: Layered Protocol Design
As per Issue #131, the system will move to a three-layer architecture:

#### 1. Communication Layer (Transport + App Protocol)
This layer handles the physical movement of bytes. It includes the carrier transport (TCP/UDP) and the application-level framing.
- **Momo-TCP**: (Existing) Legacy transport.
- **Momo-QUIC**: (New - Issue #132) Modern, secure-by-default transport using `quic-go`.
- **S3 API**: (New - Issue #133) Industry-standard interoperability (see `add-s3-protocol`).

#### 2. Core Replication Logic (Agnostic)
The core logic defines the data distribution path (e.g., `Chain`, `Splay`). This logic is **completely agnostic** of the communication layer. It executes replication by requesting a `Communicator` from the `ProtocolFactory`.

#### 3. State Management (Polymorphic System)
Determines the current replication mode based on metrics, completely separated from the network stack.

## What Changes
- Integrate `github.com/quic-go/quic-go` for QUIC support.
- Implement the `ProtocolFactory` and the `Communicator` interface in `src/common`.
- Refactor `src/server/server.go` and `src/common/replication.go` to use the transport-agnostic `Communicator`.
- Implement `MomoQUICCommunicator` to provide the existing Momo 83-byte handshake over a QUIC stream.
- Update `loadGlobalConfig` to parse `protocol=momo-quic` etc., from the `[global]` section, with a warning-level fallback to `momo-tcp`.

## Impact
- Affected specs: `replication`, `security`
- Affected code: `src/common` (dialer/listener abstractions), `src/server` (Daemon logic), `go.mod`


