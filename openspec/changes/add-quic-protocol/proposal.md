# Change: Robust Multi-Protocol Transport Layer
**Related Issues:** #132, #133

## Why
Momo currently supports a custom custom wire protocol over TCP. As the cluster grows and targets different environments (distributed WANs, cloud-native storage), the system must support multiple transport and protocol combinations (e.g., `momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`). This allows Momo to be used as a standalone high-performance replicator or as an S3-compatible gateway over reliable or lossy links.

### Architectural Rationale: Protocol/Transport Decoupling
To support multiple stacks robustly, Momo will adopt a **Factory-based Plugin Architecture**:

#### 1. Transport-Protocol Matrix
| Protocol Variant | Logic Implementation | Carrier Transport | Use Case |
| :--- | :--- | :--- | :--- |
| `momo-tcp` | Custom Momo Wire Format | Plain TCP | High-speed LAN / Legacy |
| `momo-quic` | Custom Momo Wire Format | QUIC (UDP) | Unstable WAN / Secure-by-default |
| `s3-tcp` | S3 API Compatibility | HTTP/TCP | Cloud Integration / Interop |
| `s3-quic` | S3 API Compatibility | HTTP/3 (QUIC) | Geographically Distributed S3 |

#### 2. Robust Configuration
Instead of fragmented settings, a single `protocol` field in the `[global]` section will act as a selector for the entire network stack:
```ini
[global]
# Format: <logic>-<transport>
protocol=momo-quic 
```

#### 3. Unified Abstraction
A `Transport` interface will unify `net.Conn` and `quic.Stream`, while a `ProtocolHandler` interface will encapsulate the handshake and data framing logic for both `momo` and `s3` variants.

## What Changes
- Integrate `github.com/quic-go/quic-go` for QUIC/H3 support.
- Implement a `ProtocolFactory` in `src/common` that instantiates the correct `Transport` and `ProtocolHandler` based on the `protocol` configuration.
- Add an `S3ProtocolHandler` stub for future integration (Issue #133).
- Ensure the `momo` logic is fully transport-agnostic.
- Update `loadGlobalConfig` to parse and validate the composite `protocol` string, with a warning-level fallback to `momo-tcp`.

## Impact
- Affected specs: `replication`, `security`
- Affected code: `src/common` (dialer/listener abstractions), `src/server` (Daemon logic), `go.mod`


