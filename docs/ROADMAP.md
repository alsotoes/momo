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

## 🟡 In Progress / Upcoming

| Feature | Issue | Spec | Priority | Description |
| :--- | :--- | :--- | :--- | :--- |
| **CAS Storage** | [#151](https://github.com/alsotoes/momo/issues/151) | [CAS Spec](../openspec/changes/add-cas-storage/proposal.md) | High | Implement Content-Addressable Storage for deduplication. |
| **E2E Encryption** | [#152](https://github.com/alsotoes/momo/issues/152) | [E2EE Spec](../openspec/changes/add-e2e-encryption/proposal.md) | High | Application-layer AES-GCM encryption for all file data. |
| **P2P Transport** | [#153](https://github.com/alsotoes/momo/issues/153) | [P2P Spec](../openspec/changes/add-p2p-transport/proposal.md) | Medium | Decentralized gossip-based discovery and elastic membership. |
| **Comprehensive Testing** | [#155](https://github.com/alsotoes/momo/issues/155) | [Testing Spec](../openspec/changes/add-comprehensive-testing/proposal.md) | Low | Chaos testing, distributed load generation, and observability. |

## 🔴 Future Explorations

- **Web UI Dashboard**: A real-time observability dashboard for the replication ring.
- **Auto-Balancing**: Dynamic data re-balancing when new nodes join the P2P network.
- **Client SDKs**: Native SDKs for Python and Rust.

---
*Last Updated: 2026-06-09*
