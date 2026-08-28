# Tasks: Hugo blog under docs/blog — project journey, research, architecture decisions, changes (#964)

## 1. Governance
- [x] Author OpenSpec change `blog-posts-hugo` (proposal / spec linking #964)
- [ ] Add Rule 76 to `openspec/config.yaml` (single source of truth, Rule 39)
- [ ] Add Rule 76 to `docs/AI_FLYING_SOLO.md` reference list
- [ ] Rule 70 sync: `ai_reviewer.py` flags enhancement PRs with no `docs/blog/posts/*.md` and no `no-blog` justification

## 2. Scaffold
- [ ] `docs/blog/README.md` (schema, add-post workflow, date + artifact rules)
- [ ] `docs/blog/_index.md` (Hugo section index)
- [ ] `docs/README.md` index row for blog

## 3. Posts (curated backlog, dates from anchor issue/PR createdAt or earliest code/plan commit)

### Origin & transport
- [ ] 001 origin-and-genesis (go.mod `a8114af4`, 2025-09-09) — playground → object store
- [ ] 002 replication-strategies-polymorphic (docs/REPLICATION_STRATEGIES.md, cad3663b, 2026-05-04) — chain/splay/primary-splay
- [ ] 003 transport-tcp-to-quic (PRs #763/#818, 2026-08-11) — QUIC, TLS, 0-RTT

### Storage core
- [ ] 004 cas-content-addressable-store (add-cas-storage 53000eea, 2026-03-11; PR #838 2026-08-16) — dedup, SHA-256
- [ ] 005 crush-placement (docs/CRUSH.md 37363f09, 2026-06-30; PRs #872/#873 2026-08-19) — weighted rendezvous
- [ ] 006 pluggable-storage-backends (add-pluggable-storage 45ca8fdf, 2026-07-26) — local/nfs/s3/raw
- [ ] 007 at-rest-integrity-and-gc (PRs #911/#925, 2026-08-24/25) — verify-on-read, tombstones

### S3 gateway
- [ ] 008 s3-gateway-core (PRs #782–#790, 2026-08-11) — XML, buckets, objects, lists
- [ ] 009 s3-multipart-and-breadth (PR #801, 2026-08-13; #913/#915/#921, 2026-08-24) — multipart, 501 subresources
- [ ] 010 s3-auth-presigned-sigv4 (PRs #789/#791/#885, 2026-08-11/21) — SigV4, presigned, env-decoupled keys
- [ ] 011 s3-https-tls-enforcement (PRs #792/#793, 2026-08-12) — gated insecure, TLS required
- [ ] 012 s3-integrity-checksums (PR #902, 2026-08-24) — x-amz-checksum-*

### Encryption & security (🛡 Sentinel)
- [ ] 013 e2ee-envelope-encryption (PRs #779/#781, 2026-08-11) — client envelope E2EE
- [ ] 014 confidential-dedup-oprf (PR #819, 2026-08-14; add-adaptive-auth-backoff #826, 2026-08-14) — threshold OPRF dedup, auth lockout
- [ ] 015 sentinel-security-audit (PENTESTING.md + batch issues #593–#668, 2026-08-04; PRs #811/#859/#889, 2026-08-13/21) — CRLF, smuggling, traversal, pentest CVEs

### P2P & scaling
- [ ] 016 p2p-gossip-swim (add-p2p-transport 53000eea, 2026-03-11; PRs #808/#809, 2026-08-13) — gossip, SWIM, membership
- [ ] 017 scatter-gather-lease-quorum (PRs #806/#810, 2026-08-13) — scatter-gather, leases, quorum math
- [ ] 018 adaptive-scaling-peer-quality (PRs #833/#834/#835, 2026-08-14/15) — peer quality, adaptive gossip/chunk

### Durability R1–R3
- [ ] 019 r1-failure-domain-placement (PR #952, 2026-08-26) — rack/zone/DC CRUSH
- [ ] 020 r2-degraded-read-self-heal (PR #953, 2026-08-27) — survivor fallback, rebuild
- [ ] 021 r3-write-durability-quorum (PR #954, 2026-08-27) — fsync-before-ack, group commit

### momofs R4
- [ ] 022 momofs-posix-core (PR #957, 2026-08-27; issue #932, 2026-08-25) — inode/metadata over CAS
- [ ] 023 momofs-fuse-transport (PR #963, 2026-08-28; issue #962, 2026-08-28) — bazil.org/fuse, mount

### Performance & governance (⚡ Bolt)
- [ ] 024 bolt-performance-engineering (docs/STANDARDS.md 1597efdf, 2026-06-30; PR #795, 2026-08-12) — zero-alloc, deadlines
- [ ] 025 benchmark-benchstat-gate (docs/PERFORMANCE.md auto; PRs #958/#960/#961, 2026-08-28) — three-dot gate, allowlists
- [ ] 026 metrics-observability (PR #942, 2026-08-25; add-metrics-exporter a5be9c21, 2026-07-24) — per-node bind, scrape
- [ ] 027 governance-ai-review-spec-first (add-ai-reviewer ed6798ff, 2026-06-09; PRs #909/#945, 2026-08-24/26) — Rule 73, reviewer, three-dot
- [ ] 028 roadmap-and-research (prod-ready-roadmap; issue #928, 2026-08-25; RESEARCH_PAPERS bf993e48, 2026-08-04) — R5–R11, research guide

## 4. CI
- [ ] `.github/scripts/blog_check.py` (front-matter validator: required keys, no future date, artifacts schema)
- [ ] `.github/workflows/blog_check.yml` (read-only check on PR/CHANGES)

## 5. Ship
- [ ] Sync master, open PR (`Resolves #964`), reviewer + CI, merge (Rule 52/55)
- [ ] Post-merge cleanup: delete branch (Rule 63); update #964 status comment (Rule 22)