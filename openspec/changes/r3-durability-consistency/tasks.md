# Tasks: R3 — Write durability + ack quorum + consistency (#931)

## 1. Durability ack (`src/storage`, `src/server`)
- [ ] Add fsync barrier to blob persist path; ack only after W durable replicas —
      R3-C1/R3-C2
- [ ] `durability = "fsync"|"group-commit"|"none"` 3-mode barrier; `fsync` default
- [ ] `group-commit`: amortized batch fsync before batch of acks (B5)
- [ ] `none`: best-effort buffer-ack, documented non-durable
- [ ] Write fails (no silent ack) when W unreachable durably, honoring
      `minimum_durability_factor`

## 2. Write quorum (`src/client`, `src/server`)
- [ ] `write_quorum` default 1, validated `1..=replication_factor` — R3-G1
- [ ] Client waits for W durable acks before reporting success

## 3. Consistency (`src/storage`, `src/server`)
- [ ] Per-object serialization (read-your-writes / last-ack-wins) — R3-C3
- [ ] Document non-atomicity of dispersed multi-object ops — R3-C4
- [ ] Wire interaction: controller never degrades below `write_quorum`

## 4. Config
- [ ] `durability` enum (default `"fsync"`, invalid → EINVAL), `write_quorum` (default 1)
- [ ] `conf/momo.conf` + `docs/CONFIGURATION.md` + `docs/ARCHITECTURE.md` (Rule 27)

## 5. Tests
- [ ] R3-T1 durability profile modes (mock store): fsync / group-commit / none
- [ ] R3-T2 write-quorum reaches/fails (never below floor)
- [ ] R3-T3 read-your-writes + concurrent-writer last-ack-wins
- [ ] R3-T4 goleak + `-race`

## 6. Validation
- [ ] `go fmt`, `go vet`, `go build`, `go test` (affected modules)
- [ ] `go work sync` + vendor parity
- [ ] Docs updated (consistency model + durability contract)
