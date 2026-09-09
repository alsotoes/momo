# Tasks: plugin-seam-architecture — seam-over-plugin contract for adaptive/mutating behavior (#946)

## 1. Steering rule (`openspec/config.yaml`)
- [x] Append **Rule 74 (Seam-Over-Plugins)**: adaptive/mutating behavior as
      compile-time Go interface seams (injected, registry-selected, declarative
      policy); external dynamic plugins forbidden in the data path (policy feeds
      only); fast paths concrete/zero-indirect; seams dispatch at decision
      points only; core trust invariants in auditable core (PSA-T1)

## 2. Design doc (`docs/momofs/PLUGIN_ARCHITECTURE.md`)
- [x] Positioning vs existing interface idiom + ADAPTIVE_SYSTEMS.md §12 (PSA-T2)
- [x] Two plugin kinds + perf/audit rationale (PSA-T3)
- [x] Trust core list + what stays beside it (PSA-T4)
- [x] Seam table: ReadPlanner / RebuildConverger / FS adaptor / ReplicationStrategy (PSA-T5)
- [x] Registry + declarative-policy mechanism (`atomic.Pointer`) (PSA-T6)
- [x] Perf discipline + security contract sections (PSA-T7)
- [x] Migration path (R2 then R4, reuse `sync.Once` loop) (PSA-T8)
- [x] Anti-patterns section (PSA-T9)

## 3. Seam implementations (compile-time interface seams with constructor injection)
- [x] `transport.Communicator` — wire protocol (TCP/QUIC/S3) — DONE
- [x] `p2p.Transport` — P2P cluster transport (gossip, leases, scatter-gather) — DONE
- [x] `storage.Store` — CAS storage (dedup, metadata, blob) — DONE
- [x] `storage.RebuildSource` — R2 self-heal (fetch/restore from survivors) — DONE
- [x] `storage.DurabilityBarrier` — R3 durability modes (fsync/group-commit/none) — DONE
- [x] `transport.ChecksumProvider` — R2 integrity (additive checksums) — DONE
- [x] `transport.GlobalLister` — scatter-gather list — DONE
- [x] `transport.LeaseAcquirer` — lease consensus — DONE
- [x] `transport.DeletePropagator` — P2P delete propagation — DONE
- [x] `transport.MetricsHook` — transport metrics — DONE
- [x] `transport.LatencyRecorder` — opt-in histograms — DONE
- [x] `transport.OPRFService` — threshold OPRF — DONE
- [x] `transport.ChecksumProvider` — additive checksums — DONE

## 4. Compliance tests
- [x] `src/transport/interface_compliance_test.go` — Communicator impls + no protocol imports in cluster
- [x] `src/p2p/interface_compliance_test.go` — TCPTransport + no p2p imports in cluster
- [x] `src/storage/interface_compliance_test.go` — CASStore + no storage imports in protocols
- [x] `src/server/interface_compliance_test.go` — server.go no protocol imports/assertions

## 5. OpenSpec set (Rule 11 / Rule 73)
- [x] Author `openspec/changes/plugin-seam-architecture/{proposal,tasks,spec}` linked to issue #946 (PSA-T10)

## 6. Validation
- [x] `make test` green (PSA-T11)
- [x] `git diff master...HEAD --name-only` shows only intended files (PSA-T12)
- [x] CI green including `review` (Rule 13)

## 7. Documentation
- [x] `ARCHITECTURE.md` updated with Cluster/Protocol Separation Contract section
- [x] `docs/momofs/PLUGIN_ARCHITECTURE.md` — design doc (PSA-T2-9)
- [x] `docs/adr/0031-plugin-seam-architecture.md` — ADR updated to Accepted/High
- [x] ADR 0053 created for protocol-cluster-separation

## 8. Blog post
- [x] docs/blog/posts/044-plugin-seam-architecture.md
