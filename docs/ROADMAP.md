# Momo Project Roadmap

This document outlines the high-level roadmap for the Momo project, tracking major milestones, features, and their associated GitHub issues.

## 🟢 Completed Milestones

| Feature | Issue | Status | Description |
| :--- | :--- | :--- | :--- |
| **Transport Abstraction** | [#131](https://github.com/alsotoes/momo/issues/131) | ✅ Merged | Decoupled core replication logic from the network layer. |
| **Momo-QUIC Support** | [#132](https://github.com/alsotoes/momo/issues/132) | ✅ Merged | Integrated `quic-go` for encrypted UDP-based transport. |
| **S3 Compatibility** | [#133](https://github.com/alsotoes/momo/issues/133) | ✅ Merged | Implemented S3 REST API mapping for standard tool integration. |
| **Zero-Crash Hardening** | [#134](https://github.com/alsotoes/momo/issues/134) | ✅ Merged | 100% panic protection, safe parsing, and bounded resources. |
| **Codebase Refactoring** | [#149](https://github.com/alsotoes/momo/issues/149) | ✅ Merged | Organized code into `transport`, `client`, `server`, and `common` packages. |
| **POSIX Error Mapping** | [PR #97](https://github.com/alsotoes/momo/pull/97) | ✅ Merged | Aligned application errors (Auth, Hash Mismatch) with standard `syscall` constants. |
| **Gemini AI Reviewer** | [#156](https://github.com/alsotoes/momo/issues/156) | ✅ Merged | Automated PR reviews using Gemini API to enforce steering rules. |
| **CAS Storage** | [#151](https://github.com/alsotoes/momo/issues/151) | ✅ Merged | Content-Addressable Storage with Bbolt metadata and CRUSH-lite placement. |
| **AI Reviewer 2.0** | [#156](https://github.com/alsotoes/momo/issues/156) | ✅ Merged | Automated PR reviews and autonomous merging via Gemini and GitHub Actions. |
| **Dynamic Replication** | [#165](https://github.com/alsotoes/momo/issues/165) | ✅ Merged | Configurable replication factor (1, 3, 5, etc.) with degraded mode support. |
| **Replication ID Shift** | [#158](https://github.com/alsotoes/momo/issues/158) | ✅ Merged | Re-indexed constants to set ReplicationNone as ID 0 (Internal use only). |
| **S3 Listing/Deletion** | [#225](https://github.com/alsotoes/momo/issues/225) | ✅ Merged | Implemented S3 ListObjectsV2, GetObject, and DeleteObject endpoints with progressive deadlines and bounds validation. |
| **Protocol Parity** | [#237](https://github.com/alsotoes/momo/issues/237) | ✅ Merged | Enforced Rule 33 by implementing native binary LIST, DELETE, and GET queries over both Momo-TCP and Momo-QUIC. |
| **Panic Observability** | [#245](https://github.com/alsotoes/momo/issues/245) | ✅ Merged | Aligned all 18 silent recovery blocks across TCP, QUIC, and CRUSH layers to explicitly log warnings before error propagation. |
| **P2P Transport** | [#153](https://github.com/alsotoes/momo/issues/153) | ✅ Merged | Decentralized gossip-based discovery, heartbeat liveness, and elastic membership. |
| **Decentralized P2P Scale** | [#248](https://github.com/alsotoes/momo/issues/248) | ✅ Merged | Gossip node membership, parallel Scatter-Gather queries, and Lease consensus. |
| **SWIM Failure Detection** | [#355](https://github.com/alsotoes/momo/issues/355) | ✅ Merged | SWIM-style ping/ack, indirect ping, RTT EWMA, and adaptive timeouts. |
| **CAS Garbage Collection** | [#350](https://github.com/alsotoes/momo/pull/350) | ✅ Merged | Reference-counted GC with tombstone retention and P2P delete propagation via scatter-gather. |
| **Comprehensive Testing** | [#155](https://github.com/alsotoes/momo/issues/155) | ✅ Merged | k6 load/stress/chaos tests, chaos engineering scripts, Prometheus/Grafana monitoring, K8s scalability, TCP contract tests, context.WithTimeout refactoring. |
| **Prometheus Metrics Exporter** | [#364](https://github.com/alsotoes/momo/issues/364) | ✅ Merged | Built-in Prometheus `/metrics` endpoint with `sync/atomic` counters, `MetricsHook` interface for transport-layer instrumentation, zero-overhead on hot path. |
| **Scanner-Safe Secrets (Rule 29)** | [#216](https://github.com/alsotoes/momo/issues/216) | ✅ Merged | `notsecret` annotation enforcement on all dummy tokens via pre-commit hook and CI check. |
| **Pluggable Storage Backends** | [#226](https://github.com/alsotoes/momo/issues/226) | ✅ Merged | Configurable blob storage (local, NFS, S3, raw) via `BlobStore` interface with zero-dep SigV4 client and bbolt metadata. |
| **Storage Backend E2E Tests** | [#409](https://github.com/alsotoes/momo/issues/409) | ✅ Merged | E2E integration tests for S3 and raw device backends with CI workflow. |
| **Path Traversal Hardening** | [#410](https://github.com/alsotoes/momo/issues/410) | ✅ Merged | Sanitized scatter-gather query handlers with bounds validation (Rule 32) and panic recovery (Rule 37). |

## 🟡 In Progress / Upcoming

| Feature | Issue | Spec | Priority | Description |
| :--- | :--- | :--- | :--- | :--- |
| **E2E Encryption** | [#152](https://github.com/alsotoes/momo/issues/152) | [E2EE Spec](../openspec/changes/add-e2e-encryption/specs/security/spec.md) | High | Client-side zero-knowledge AES-GCM-256 encryption for all stored files. Options analysis posted in issue — S3 compatibility vs. zero-knowledge tradeoff under review. |
| **Metrics Exporter Phase 2-4** | [#364](https://github.com/alsotoes/momo/issues/364) | [Metrics Spec](../openspec/changes/add-metrics-exporter/specs/observability/spec.md) | Medium | Storage metrics (disk/CAS), P2P/cluster gauges, opt-in latency histograms. Phase 1 (wiring) complete. |

## 🔴 Future Explorations

- **Web UI Dashboard**: A real-time observability dashboard for the replication ring.
- **Auto-Balancing**: Dynamic data re-balancing when new nodes join the P2P network.
- **Client SDKs**: Native SDKs for Python and Rust.

---
*Last Updated: 2026-07-26*
