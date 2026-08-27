# Tasks: R3 — Write durability + ack quorum + consistency (#931)

## 1. Durability ack (`src/storage`, `src/server`)
- [x] Add fsync/group-commit/none barrier to blob persist path; ack only after
      the durable barrier crosses (multi-replica W counting = client follow-up)
- [x] `durability = "fsync"|"group-commit"|"none"` 3-mode barrier; `fsync` default
- [x] `group-commit`: amortized batch fsync before batch of acks (B5)
- [x] `none`: best-effort buffer-ack, documented non-durable
- [x] Write fails (no silent ack) when the durability barrier fails (storage), honoring
      `minimum_durability_factor`

## 2. Write quorum (`src/client`, `src/server`)
- [x] `write_quorum` default 1, validated `1..=replication_factor` — R3-G1
- [ ] Client waits for W durable acks before reporting success

## 3. Consistency (`src/storage`, `src/server`)
- [x] Per-object serialization (read-your-writes / last-ack-wins) — R3-C3
- [x] Document non-atomicity of dispersed multi-object ops — R3-C4
- [ ] Wire interaction: controller never degrades below `write_quorum`

## 4. Config
- [x] `durability` enum (default `"fsync"`, invalid → EINVAL), `write_quorum` (default 1)
- [x] `conf/momo.conf` + `docs/CONFIGURATION.md` + `docs/ARCHITECTURE.md` (Rule 27)

## 5. Tests
- [x] R3-T1 durability profile modes (mock store): fsync / group-commit / none
- [ ] R3-T2 write-quorum reaches/fails (never below floor)
- [x] R3-T3 read-your-writes + concurrent-writer last-ack-wins
- [x] R3-T4 goleak + `-race`

## 6. Validation
- [x] go vet, go build, go test (affected modules) green
- [ ] `go work sync` + vendor parity
- [x] Docs updated (consistency model + durability contract)
