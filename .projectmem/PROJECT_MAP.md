# Project Map - momo

Momo is a high-performance, zero-SPOF distributed Object Storage system in Go ("Read From Any Node") supporting dynamic runtime replication switching (None, Chain, Splay, Primary-Splay), CRUSH-lite placement, and multi-protocol wire parity (`momo-tcp`, `momo-quic`, `s3-tcp`, `s3-quic`).

## Stack
- Tags: go, distributed-storage, s3, quic, tcp, docker, github-actions
- Key libraries: `gopkg.in/ini.v1`, `github.com/shirou/gopsutil/v3`, `github.com/quic-go/quic-go`, `go.uber.org/goleak`
- Tooling & Harness: OpenSpec (`openspec/`), CodeGraph (`.codegraph/`), ProjectMem (`.projectmem/`), Context-Mode (`ctx_*`), RTK (`rtk`), Ponytail (`lite`)

## Structure
- `src/` — Go packages (`server`, `client`, `transport`, `common`, `storage`, `metrics`, `crypto`, `fuse`)
- `openspec/` — Single source of truth for steering rules (`config.yaml`), harness manifest (`ai-workflow.yaml`), and feature specs (`changes/`)
- `docs/` — Core principles (`CORE/`), architecture (`ARCHITECTURE.md`), ADRs (`adr/`), Hugo blog (`blog/`), operational guides (`GUIDES/`)
- `tools/` — ADR sync (`adr-sync`), benchmark and doc generators
- `tests/` & `pentest/` — Integration, E2E, and security fuzzing suites
- `conf/` — INI cluster and node configuration files

## Suggested First Reads
- `openspec/config.yaml` — Project Steering Rules (Rule 39 single source of truth)
- `openspec/ai-workflow.yaml` — Agent & plugin harness manifest
- `docs/CORE/DESIGN_PRINCIPLES.md` & `docs/CORE/STANDARDS.md` — Core tenets, Bolt performance, Sentinel security
- `docs/AI_FLYING_SOLO.md` — Autonomous development and PR workflow
