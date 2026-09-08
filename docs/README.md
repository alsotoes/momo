# Momo

Momo is a high-performance, transport-agnostic **distributed object storage system** written in Go. It stores content-addressed blobs (SHA-256) with server-side deduplication, replicates them across a cluster using pluggable replication strategies, and exposes multiple access surfaces: a native TCP/QUIC protocol and an S3-compatible REST gateway, plus a FUSE filesystem (`momofs`) for POSIX access.

It is designed around a small, auditable core: CRUSH-lite placement, a pluggable `BlobStore` backend layer, P2P gossip membership, and runtime-reconfigurable replication (a "polymorphic" system driven by live metrics).

## Documentation Index

| Document | Description |
|---|---|
| [ARCHITECTURE.md](ARCHITECTURE.md) | System architecture, storage layer, replication, P2P, metrics |
| [CONFIGURATION.md](CONFIGURATION.md) | Complete configuration reference for `momo.conf` |
| [STANDARDS.md](STANDARDS.md) | ⚡ Bolt (performance) and 🛡️ Sentinel (security) coding standards |
| [PROTOCOL.md](PROTOCOL.md) | Wire protocol specification (handshake, metadata, replication) |
| [REPLICATION_STRATEGIES.md](REPLICATION_STRATEGIES.md) | Chain, Splay, Primary-Splay replication modes |
| [CRUSH.md](CRUSH.md) | CRUSH-lite placement algorithm (Weighted Rendezvous Hashing) |
| [P2P.md](P2P.md) | P2P gossip, SWIM failure detection, scatter-gather, lease consensus |
| [TESTING.md](TESTING.md) | Test suites, CI pipeline, contract testing, E2E tests |
| [CONTRIBUTING.md](CONTRIBUTING.md) | Contribution guidelines and PR workflow |
| [ROADMAP.md](ROADMAP.md) | Project roadmap with milestones and GitHub issues |
| [ERROR_CODES.md](ERROR_CODES.md) | POSIX error codes and exit statuses reference |
| [PERFORMANCE.md](PERFORMANCE.md) | Auto-generated benchmark results and performance history |
| [COMPATIBILITY.md](COMPATIBILITY.md) | Go version, platform compatibility, dependencies |
| [CONTRACT_TESTING.md](CONTRACT_TESTING.md) | TCP wire protocol contract testing strategy |
| [POLYMORPHIC_SYSTEM.md](POLYMORPHIC_SYSTEM.md) | Dynamic replication mode switching and polymorphic engine |
| [AI_FLYING_SOLO.md](AI_FLYING_SOLO.md) | Autonomous development workflow for AI agents (bugs and features) |
| [EXTERNAL_CLIENT_REPLICATION.md](EXTERNAL_CLIENT_REPLICATION.md) | External S3 client replication mode downgrade handling |
| [PENTESTING.md](PENTESTING.md) | Security pentest overview — DotDotPwn fuzzing + Python exploit scripts (points to `pentest/README.md` for reproduction) |
| [blog/README.md](blog/README.md) | Engineering journal (Hugo-format posts) — journey, research, architecture decisions, changes |
| [adr/README.md](adr/README.md) | Architecture Decision Records (Fowler pattern) — one per ratified OpenSpec change, auto-synced from specs |

## Key Features

- **Content-Addressable Storage (CAS)**: blobs identified by SHA-256 content hash, with server-side deduplication and at-rest integrity verification (verify-on-read + background scrub).
- **Pluggable Storage Backends**: `local` (default), `nfs`, `s3` (zero-dependency SigV4 client), and `raw` (direct block I/O) via the `BlobStore` interface. Metadata lives in a per-node Bbolt store.
- **Pluggable Transports**: native TCP and QUIC (TLS 1.3), plus an S3-compatible REST gateway, through a modular `ProtocolFactory`.
- **CRUSH-lite Placement**: deterministic, failure-domain-aware replica placement (Weighted Rendezvous Hashing) computed client-side — no central metadata service.
- **Replication Strategies**: runtime-switchable `None`, `Chain`, `Splay`, and `Primary-Splay` modes, reconfigurable by a metrics-driven "polymorphic" controller.
- **Write Durability & Quorum**: configurable durability barrier (`fsync` / `group-commit` / `none`) and `write_quorum` semantics.
- **P2P Cluster Coordination**: gossip membership with SWIM-style failure detection, scatter-gather queries, and lease-based consensus for deletes.
- **Distributed Metadata (R6)**: consistent-hash ring with quorum reads/writes, vector clocks, and shard-aware listing — optional and backward compatible.
- **S3 Gateway**: SigV4 auth (dedicated gateway credentials), multipart upload, checksums, honest `501` for unsupported subresources.
- **FUSE Filesystem (momofs)**: POSIX access to the CAS store via `momo -imp fs` (go-fuse/v2).
- **Security**: mandatory 64-byte auth-token validation, CRLF injection protection, path-traversal validation, envelope E2EE for clients, honest panic recovery (Rule 37).
- **Prometheus Metrics**: `/metrics` + `/health` per node with `sync/atomic` counters — zero overhead on hot paths.

## Repository Layout

| Path | What lives here |
|---|---|
| `src/` | Go source: `transport/` (TCP, QUIC, S3), `storage/` (CAS + blob stores), `server/` (daemon + metrics exporter), `client/`, `p2p/` (gossip, scatter-gather, leases), `crypto/` (E2EE, OPRF), `momofs/` (FUSE), `common/`, `metrics/` |
| `src/momo.go` | Entry point — client/server runner and metrics bootstrap |
| `.github/` | CI/CD (19 workflows: build, test, benchmark, smoke, pentest, reviewer) + governance scripts (`ai_reviewer.py`, E2E runners) |
| `docs/` | This documentation set (architecture, protocol, config, testing, ADRs, engineering blog) |
| `openspec/` | OpenSpec changes (`changes/`), ratified specs, `github.yaml` lifecycle binding |
| `conf/` | Example configurations (`momo.conf`, `smoke.conf`, `pentest.conf`) |
| `pentest/` | Security pentest toolkit — DotDotPwn fuzzing + Python exploit scripts |
| `hooks/` | Git hooks (pre-commit benchmark/doc regeneration) |

## Getting Started

```bash
make build     # build the momo binary
make test      # run all unit + integration tests
```

See [CONFIGURATION.md](CONFIGURATION.md) for `momo.conf` reference and `conf/momo.conf` for a working example. A 3-node cluster can be brought up with `docker compose up` (see [TESTING.md](TESTING.md) for the smoke/E2E matrix).

## Verification

- `make test` — full unit + integration suite (race detector + `goleak`).
- `make smoke-*` — end-to-end replication over TCP/QUIC/S3 across virtual daemons.
- `make test-metrics` / `make test-contract` — metrics and wire-protocol contract E2E.
- `make pentest` — security pentest (DotDotPwn + Python exploits).

Full detail in [TESTING.md](TESTING.md); benchmark history in [PERFORMANCE.md](PERFORMANCE.md).