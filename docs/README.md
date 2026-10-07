# Momo

<p align="center">
  <a href="https://github.com/alsotoes/momo/actions/workflows/go.yml"><img src="https://github.com/alsotoes/momo/actions/workflows/go.yml/badge.svg" alt="Go CI" height="20" /></a>
  <a href="https://pkg.go.dev/github.com/alsotoes/momo"><img src="https://pkg.go.dev/badge/github.com/alsotoes/momo.svg" alt="Go Reference" height="20" /></a>
  <a href="https://github.com/alsotoes/momo/blob/master/go.mod"><img src="https://img.shields.io/github/go-mod/go-version/alsotoes/momo" alt="Go Version" height="20" /></a>
  <a href="https://github.com/alsotoes/momo/blob/master/LICENSE"><img src="https://img.shields.io/badge/License-GPLv3-blue.svg" alt="License: GPL v3" height="20" /></a>
</p>

<p align="center">
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=alert_status" alt="Quality Gate Status" height="20" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/ai_code_assurance?project=alsotoes_momo" alt="AI Code Assurance" height="20" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=security_rating" alt="Security Rating" height="20" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=vulnerabilities" alt="Vulnerabilities" height="20" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=bugs" alt="Bugs" height="20" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=coverage" alt="Coverage" height="20" /></a>
</p>

<p align="center">
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=ncloc" alt="Lines of Code" height="20" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=code_smells" alt="Code Smells" height="20" /></a>
  <a href="https://sonarcloud.io/summary/new_code?id=alsotoes_momo"><img src="https://sonarcloud.io/api/project_badges/measure?project=alsotoes_momo&metric=duplicated_lines_density" alt="Duplicated Lines" height="20" /></a>
  <img src="https://visitor-badge.laobi.icu/badge?page_id=alsotoes.momo" alt="Visitors" height="20" />
</p>

Momo is a high-performance, transport-agnostic **distributed object storage system** written in Go. It stores content-addressed blobs (SHA-256) with server-side deduplication, replicates them across a cluster using pluggable replication strategies, and exposes multiple access surfaces: a native TCP/QUIC protocol and an S3-compatible REST gateway, plus a FUSE filesystem (`momofs`) for POSIX access.

It is designed around a small, auditable core: CRUSH-lite placement, a pluggable `BlobStore` backend layer, P2P gossip membership, and runtime-reconfigurable replication (a "polymorphic" system driven by live metrics).

## Foundational Principles & Architecture

Momo is engineered around two foundational philosophies that govern all subsystem designs:

1. **[Core Design Principles](CORE/DESIGN_PRINCIPLES.md)** — 14 core tenets of Momo, centered on **Read From Any Node**, zero coordinator bottlenecks, customer-transparent complexity, self-healing, and cloud/HPC readiness.
2. **[Adaptive Systems Design](CORE/ADAPTIVE_SYSTEMS.md)** — 16 biological models (Ant Colony Optimization, Epigenetics, Homeostasis, Stigmergy, Immune System, Apoptosis) enabling the cluster to self-tune and heal without human intervention.
3. **[Engineering Standards (⚡ Bolt & 🛡️ Sentinel)](CORE/STANDARDS.md)** — Predictable zero-allocation performance and zero-trust defensive security invariants.
4. **[Architectural Decisions (DD-1 to DD-6)](CORE/DESIGN_DECISIONS.md)** — Foundational trade-offs on embedded BoltDB, dual independent rings, and wrapper seams.
5. **[Distributed Storage Comparison](CORE/COMPARISON.md) & [Lessons Learned](CORE/LESSONS_LEARNED.md)** — In-depth architectural analysis and lessons learned from Ceph, Lustre, ScyllaDB, and IPFS.

## Documentation Index

| Document | Description |
|---|---|
| [DESIGN_PRINCIPLES](CORE/DESIGN_PRINCIPLES.md) | 14 core tenets: Read From Any Node, zero SPOF, self-healing, HPC/Cloud ready |
| [ADAPTIVE_SYSTEMS](CORE/ADAPTIVE_SYSTEMS.md) | 16 bio-inspired models: Ant Colony routing, Epigenetic concurrency, Homeostasis |
| [STANDARDS](CORE/STANDARDS.md) | ⚡ Bolt (performance) and 🛡️ Sentinel (security) coding standards |
| [ARCHITECTURE](ARCHITECTURE.md) | System architecture, storage layer, replication, P2P, metrics |
| [DESIGN_DECISIONS](CORE/DESIGN_DECISIONS.md) | Architectural Decision Records DD-1 to DD-6 (embedded DB, dual rings, seams) |
| [COMPARISON](CORE/COMPARISON.md) | Feature matrix vs Ceph, Lustre, ScyllaDB, IPFS |
| [LESSONS_LEARNED](CORE/LESSONS_LEARNED.md) | Architectural takeaways from mature distributed storage engines |
| [CONFIGURATION](GUIDES/CONFIGURATION.md) | Complete configuration reference for `momo.conf` |
| [PROTOCOL](REFERENCE/PROTOCOL.md) | Wire protocol specification (handshake, metadata, replication) |
| [REPLICATION_STRATEGIES](REFERENCE/REPLICATION_STRATEGIES.md) | Chain, Splay, Primary-Splay replication modes |
| [CRUSH](REFERENCE/CRUSH.md) | CRUSH-lite placement algorithm (Weighted Rendezvous Hashing) |
| [P2P](REFERENCE/P2P.md) | P2P gossip, SWIM failure detection, scatter-gather, lease consensus |
| [MOMOFS](momofs/README.md) | MomoFS FUSE/POSIX filesystem subsystem, mount user guide, CAS inode schema |
| [TESTING](GUIDES/TESTING.md) | Test suites, CI pipeline, contract testing, E2E tests |
| [CONTRIBUTING](GUIDES/CONTRIBUTING.md) | Contribution guidelines and PR workflow |
| [ROADMAP](GUIDES/ROADMAP.md) | Project roadmap with milestones and GitHub issues |
| [ERROR_CODES](REFERENCE/ERROR_CODES.md) | POSIX error codes and exit statuses reference |
| [PERFORMANCE](REFERENCE/PERFORMANCE.md) | Auto-generated benchmark results and performance history |
| [COMPATIBILITY](REFERENCE/COMPATIBILITY.md) | Go version, platform compatibility, dependencies |
| [CONTRACT_TESTING](GUIDES/CONTRACT_TESTING.md) | TCP wire protocol contract testing strategy |
| [POLYMORPHIC_SYSTEM](REFERENCE/POLYMORPHIC_SYSTEM.md) | Dynamic replication mode switching and polymorphic engine |
| [AI_FLYING_SOLO](AI_FLYING_SOLO.md) | Autonomous development workflow for AI agents (bugs and features) |
| [EXTERNAL_CLIENT_REPLICATION](GUIDES/EXTERNAL_CLIENT_REPLICATION.md) | External S3 client replication mode downgrade handling |
| [PENTESTING](GUIDES/PENTESTING.md) | Security pentest overview — DotDotPwn fuzzing + Python exploit scripts (points to `pentest/README.md` for reproduction) |
| [BLOG](blog/README.md) | Engineering journal (Hugo-format posts) — journey, research, architecture decisions, changes |
| [ADR](adr/README.md) | Architecture Decision Records (Fowler pattern) — one per ratified OpenSpec change, auto-synced from specs |
| [ECC_TOOLS](ECC_TOOLS.md) | ECC Tools GitHub App — advisory PR audits, reviewer integration, bundle triage |

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
| `tools/` | Internal developer tooling and helper scripts (e.g. `adr-sync`, `run_sonarq_scan.sh`) |
| `.jules/` | Learning files (`bolt.md`, `sentinel.md`) and instructions for Jules agent (`instructions.md`) |
| `JULES.md` | Autonomous agent instruction pointer for Google Labs Jules |
| `sonar-project.properties` | SonarCloud/SonarQube static analysis and test coverage configuration |
| `.sonarcloud.properties` | SonarCloud Automatic Analysis scope and exclusion configuration |

## Getting Started

```bash
make build     # build the momo binary
make test      # run all unit + integration tests
```

See [GUIDES/CONFIGURATION.md](GUIDES/CONFIGURATION.md) for `momo.conf` reference and `conf/momo.conf` for a working example. A 3-node cluster can be brought up with `docker compose up` (see [GUIDES/TESTING.md](GUIDES/TESTING.md) for the smoke/E2E matrix).

## Verification

- `make test` — full unit + integration suite (race detector + `goleak`).
- `make smoke-*` — end-to-end replication over TCP/QUIC/S3 across virtual daemons.
- `make test-metrics` / `make test-contract` — metrics and wire-protocol contract E2E.
- `make pentest` — security pentest (DotDotPwn + Python exploits).

Full detail in [GUIDES/TESTING.md](GUIDES/TESTING.md); benchmark history in [REFERENCE/PERFORMANCE.md](REFERENCE/PERFORMANCE.md).
