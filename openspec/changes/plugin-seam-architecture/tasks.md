# Tasks: plugin-seam-architecture — seam-over-plugin contract for adaptive/mutating behavior (#946)

## 1. Steering rule (`openspec/config.yaml`)
- [ ] Append **Rule 74 (Seam-Over-Plugins)**: adaptive/mutating behavior as
      compile-time Go interface seams (injected, registry-selected, declarative
      policy); external dynamic plugins forbidden in the data path (policy feeds
      only); fast paths concrete/zero-indirect; seams dispatch at decision
      points only; core trust invariants in auditable core (PSA-T1)

## 2. Design doc (`docs/momofs/PLUGIN_ARCHITECTURE.md`)
- [ ] Positioning vs existing interface idiom + ADAPTIVE_SYSTEMS.md §12 (PSA-T2)
- [ ] Two plugin kinds + perf/audit rationale (PSA-T3)
- [ ] Trust core list + what stays beside it (PSA-T4)
- [ ] Seam table: ReadPlanner / RebuildConverger / FS adaptor / ReplicationStrategy (PSA-T5)
- [ ] Registry + declarative-policy mechanism (`atomic.Pointer`) (PSA-T6)
- [ ] Perf discipline + security contract sections (PSA-T7)
- [ ] Migration path (R2 then R4, reuse `sync.Once` loop) (PSA-T8)
- [ ] Anti-patterns section (PSA-T9)

## 3. OpenSpec set (Rule 11 / Rule 73)
- [x] Author `openspec/changes/plugin-seam-architecture/{proposal,tasks,spec}`
      linked to issue #946 (PSA-T10)

## 4. Validation
- [ ] `make test` green (PSA-T11)
- [ ] `git diff master...HEAD --name-only` shows only config.yaml, the new doc,
      and this OpenSpec set (PSA-T12)
- [ ] CI green including `review` (Rule 13)
