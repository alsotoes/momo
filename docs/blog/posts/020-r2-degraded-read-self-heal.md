---
title: "R2: Degraded-Read Survivor Fallback and Self-Heal"
date: 2026-08-27T10:14:18Z
draft: false
post_type: architecture
tags: [go, durability, selfheal, rebuild, sentinel]
categories: [durability]
summary: "When a local replica is corrupted or missing, degraded read fetches from healthy survivors transparently, while a background rebuild loop restores cluster redundancy."
artifacts:
  - {type: pr, id: "953"}
  - {type: spec, path: openspec/changes/r2-self-heal-rebuild}
  - {type: issue, id: "928"}
related:
  - 007-at-rest-integrity-and-gc
  - 017-scatter-gather-lease-quorum
  - 018-adaptive-scaling-peer-quality
  - 019-r1-failure-domain-placement
  - 021-r3-write-durability-quorum
  - 044-plugin-seam-architecture
---

In distributed storage, disks do not merely fail catastrophically by dropping offline—they rot incrementally. A single bad sector can corrupt a 4 KB block inside a 50 MB video file.

Before milestone **R2** (issue #928 / PR #953), if a client's request landed on a node whose local disk block was corrupted or missing, `CASStore.Get` returned `syscall.EBADMSG` or `syscall.ENOENT`. The client experienced a hard failure, even though two perfectly healthy replicas existed on peer nodes in the cluster.

Milestone R2 solved this by introducing **transparent degraded reads** and a **background self-heal rebuild loop** governed by a compile-time **Rule 74 seam** (`RebuildSource`).

```
========================================================================================
                          TRANSPARENT DEGRADED READ & REBUILD
========================================================================================

 Client GET /data/file.bin
         |
         v
 [ Node 0 (Local Primary) ]
         |
         v
 Opens blobs/e3/b0/c4/... -> DISK CORRUPTION DETECTED (syscall.EBADMSG)
         |
         +-------------------------------------------------------+
         |                                                       |
   (Without R2)                                              (With R2)
         |                                                       |
         v                                                       v
 Fail client with 500 / EBADMSG                     1. Intercept EBADMSG
 (Availability Lost!)                               2. Mark local blob in bucketQuarantine
                                                    3. Query RebuildSource.Survivors(hash)
                                                             |
                                                             v
                                                    [ Dial Node 1 (Survivor) ]
                                                             |
                                                             v
                                                    Stream verified payload to Client
                                                    (Client experiences 0 errors!)
                                                             |
                                                             v
                                                    4. Queue Async Self-Heal Rebuild:
                                                       Restore healthy blob to Node 0 disk
```

---

## 1. The Seam Architecture: `RebuildSource`

In accordance with **Rule 74 (Seam-Over-Plugins)**, the storage engine must not be directly coupled to cluster transports, network sockets, or CRUSH placement logic. In `src/storage/rebuild.go`, momo defines the `RebuildSource` interface seam:

```go
type Peer struct {
    ID     int
    Domain string // R1 failure domain
}

type RebuildSource interface {
    // Survivors returns peer daemons holding verified copies of the blob.
    Survivors(hash string) ([]Peer, error)

    // Fetch opens a verify-before-use stream from a healthy survivor.
    Fetch(hash string, survivor Peer) (io.ReadCloser, error)

    // Restore pushes verified bytes to repair under-replicated nodes.
    Restore(hash string, content io.Reader) error
}
```

The server daemon wires this seam using CRUSH placement and peer RPCs, while unit tests inject deterministic in-memory mock survivors.

---

## 2. Transparent Degraded-Read Intercept

Inside `CASStore.Get()` (`src/storage/storage.go`), the read pipeline transparently recovers from local disk corruption:

```go
stream, meta, err := s.blobs.GetBlob(hash)
if err != nil || corruptOnRead {
    // Local read failed (ENOENT or EBADMSG bitrot)
    if s.rebuildConfig.DegradedRead && s.rebuildSource != nil {
        log.Printf("R2: Local blob %s corrupt or missing; attempting degraded read", hash)

        // 1. Find surviving peers holding valid copies
        survivors, sErr := s.rebuildSource.Survivors(hash)
        if sErr == nil && len(survivors) > 0 {
            // 2. Fetch verified stream from the first healthy survivor
            remoteStream, fErr := s.rebuildSource.Fetch(hash, survivors[0])
            if fErr == nil {
                // 3. Quarantine corrupt local block to prevent re-serving
                _ = s.quarantineBlob(hash)

                // 4. Return remote stream directly to client
                return remoteStream, meta, nil
            }
        }
    }
    return nil, meta, fmt.Errorf("read failed and no survivors available: %w", err)
}
```

The client receives the complete, verified stream with no protocol errors. The slight bump in Time-to-First-Byte (TTFB) is the only observable artifact of the underlying disk failure.

---

## 3. The Background Self-Heal Rebuild Loop

Surviving a degraded read is only half the battle: the cluster's replica count is now at $R-1$, leaving it vulnerable to a second failure.

In `src/storage/rebuild.go`, `StartRebuild` runs an asynchronous reconciliation loop:
1. **Inventory**: The worker scans Bbolt's `bucketObjects` for active blobs.
2. **Quorum Audit**: It queries `RebuildSource.Survivors(hash)` to determine the current healthy replica count.
3. **Restoration**: If the healthy replica count is below the configured target ($R$), the worker fetches a verified stream from a survivor and calls `RebuildSource.Restore()`.
4. **Failure-Domain Awareness**: New replica placements prioritize unoccupied physical domains (R1), ensuring restored copies do not share rack infrastructure with existing survivors.

---

## 4. Tradeoff Analysis

| Strategy | Client Impact | Network Overhead | Recovery Speed |
|---|---|---|---|
| **Fail-Stop (No R2)** | Immediate client 500 error | Zero | Manual operator intervention |
| **Immediate Synchronous Repair** | High latency spike (client waits for local re-write) | Spikes on user thread | Immediate |
| **Degraded Read + Async Self-Heal (momo R2)** | **Zero errors; minor TTFB increase on first read** | **Smooth background stream** | **Autonomous background convergence** |

---

## 5. Engineering Standards: 🛡 Sentinel & ⚡ Bolt

Per [docs/STANDARDS.md](../../STANDARDS.md):
- 🛡 **Sentinel (Verify-Before-Use)**: `Fetch()` enforces strict streaming verification. A survivor's bytes are never accepted or written to local disk without re-deriving the SHA-256 hash. Bad data on one node can never propagate to heal another.
- ⚡ **Bolt (Bounded Concurrency)**: The rebuild loop uses a bounded worker pool (`RebuildConfig.Workers`, default 2) to prevent background repair traffic from starving customer read/write IOPS.

## Related

- Integrity & background scrubbing: [007](007-at-rest-integrity-and-gc.md)
- Domain-aware placement: [019](019-r1-failure-domain-placement.md)
- Write durability & quorum: [021](021-r3-write-durability-quorum.md)
- Seams architecture: [044](044-plugin-seam-architecture.md)
