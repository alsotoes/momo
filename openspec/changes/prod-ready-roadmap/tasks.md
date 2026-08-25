# Tasks: Production-readiness roadmap — prioritized hardening track (#928)

## 1. Ratify roadmap
- [x] Proposal written (`proposal.md`) with P0/P1/P2 phases, item table, DoD
- [x] Specification written (`spec.md`) with REQ-1..REQ-11 + phase gates
- [x] Link each item to its OpenSpec change + GitHub issue
- [ ] docs/ROADMAP.md updated with the production-readiness tiers (Rule 27)
- [ ] Mirror to GitHub: create parent roadmap issue + sub-issue chain R1–R11
      (labels `enhancement` + `automation`, assignee `alsotoes`) — Rule 11/49

## 2. Phase P0 items (separate OpenSpec changes + PRs)
- [ ] R1 failure-domain-aware CRUSH placement
- [ ] R2 degraded-read + self-heal rebuild
- [ ] R3 write durability + ack quorum + consistency model
- [ ] R4 momofs FUSE/POSIX filesystem layer

## 3. Phase P1 items
- [ ] R5 metrics phases 2–4 + dashboards/alerts
- [ ] R6 metadata catalog HA + backup/recovery
- [ ] R7 error model + ops (ENOSPC, exit codes, cluster health)
- [ ] R8 multi-tenancy + authorization + audit
- [ ] R9 secrets management + key rotation

## 4. Phase P2 items
- [ ] R10 S3 lifecycle/versioning/notification/lock breadth (#820)
- [ ] R11 auto-rebalance on membership change

## 5. Validation
- [ ] Each sub-change: `go test -race`, goleak, Rule-27 docs, CI green
- [ ] Master branch CI green before each new item branch (Rule 71)
