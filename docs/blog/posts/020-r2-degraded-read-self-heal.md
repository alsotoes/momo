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

A user reported that a video file played fine one minute and then, without any
warning, returned a server error. Nothing had changed. No node was down, no
deploy had shipped. One sector on one disk had quietly rotted, and the request
happened to land on that disk.

Disks do not merely fail catastrophically by dropping offline — they also rot
incrementally. A single bad sector can corrupt a 4 KB block in the middle of a
50 MB video while the rest of the file looks perfectly healthy. We call this
**bitrot**: silent, localized corruption that a disk reports only when you
happen to read the affected bytes.

## The Real Problem

Before milestone **R2**, if a client's read landed on a node whose local disk
block was corrupted or missing, the storage engine surfaced a raw system error
and gave up. The client saw a hard failure, even though two perfectly healthy
copies of the object existed on peer nodes elsewhere in the cluster. We had
redundancy we were not using at read time.

The fix had to do two things: serve the read anyway, from a healthy peer, and
then repair the damaged node so the cluster returned to full redundancy without
a human in the loop. That second half is what we call **self-heal** — the cluster
detecting its own under-replication and autonomously converging back to the
target number of copies.

## Why the Obvious Solutions Failed

**"Return the error."** This preserves correctness at the cost of availability.
We had the data; refusing to serve it makes our redundancy worthless exactly when
it matters.

**"Repair synchronously, then serve."** We considered blocking the client while
we re-wrote the corrupt block locally. But the repair might need to fetch a large
object across the network, turning a read into a long stall on the user's thread.

**"Ignore corruption and keep serving local bytes."** This is the worst option.
Serving rotted data silently corrupts the objects the cluster is supposed to
protect.

We chose a third path: serve from a healthy survivor immediately, explain the
corruption to no one, and repair in the background.

## The Solution: A Seam for Survivors

The storage engine must not be tightly coupled to cluster transports, network
sockets, or placement logic — otherwise we could never test the recovery logic
without a live cluster. Following our **seam** discipline (a compile-time
interface that marks the one place a subsystem is allowed to plug into another),
we defined a `RebuildSource` interface as the boundary between the read path and
the cluster:

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

Production wires this seam to placement and peer RPCs. Tests inject deterministic
in-memory mock survivors, so the recovery logic is exercised without a network.

### Transparent degraded read

Inside the read path, when a local read fails, we intercept the error and try the
cluster instead:

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

The client receives the complete, verified stream with no protocol error. The only
observable artifact of the underlying disk failure is a slightly higher
Time-to-First-Byte (TTFB) — the delay before the first byte of the response
arrives. We **quarantine** the bad local block — mark it unusable so it is never
served again — rather than deleting it, so operators can still inspect the
damage.

### The background self-heal loop

Surviving a degraded read is only half the battle. The cluster is now running at
one replica fewer than its target, which leaves it vulnerable to a second
failure. A background reconciliation loop repairs the gap:

1. **Inventory.** The worker walks the object index for active blobs.
2. **Audit.** For each blob it asks the cluster how many healthy copies currently
   exist.
3. **Restore.** If that count is below the configured target, it fetches a
   verified stream from a survivor and pushes it to a node that needs the copy.
4. **Domain awareness.** The new copy's placement prefers unoccupied failure
   domains, from [019](019-r1-failure-domain-placement.md), so a restored copy
   does not land in the same rack as every survivor.

## The Trade-off

The diagram below traces what happens on a corrupt local read, with and without
R2:

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

| Strategy | Client Impact | Network Overhead | Recovery Speed |
|---|---|---|---|
| **Fail-Stop (No R2)** | Immediate client 500 error | Zero | Manual operator intervention |
| **Immediate Synchronous Repair** | High latency spike (client waits for local re-write) | Spikes on user thread | Immediate |
| **Degraded Read + Async Self-Heal (momo R2)** | **Zero errors; minor TTFB increase on first read** | **Smooth background stream** | **Autonomous background convergence** |

We accepted degraded-read complexity and background network traffic in exchange
for an availability guarantee. The cost is real: a corrupted node now generates
repair traffic, and a degraded read is slower than a healthy one. But a user
watching a video never sees the failure.

## How We Verified

Per [docs/CORE/STANDARDS.md](../../STANDARDS.md):

- 🛡 **Sentinel (verify-before-use)**: `Fetch()` enforces strict streaming
  verification. A survivor's bytes are never accepted or written to local disk
  without re-deriving the object's SHA-256 hash. Bad data on one node can never
  propagate to heal another — we verify before we trust, every time.
- ⚡ **Bolt (bounded concurrency)**: the rebuild loop runs a small bounded worker
  pool (default two workers) so background repair traffic cannot starve customer
  read and write IOPS.

## What Could Go Wrong

- **Every survivor is corrupt.** If no healthy copy remains, the degraded read
  fails; R2 cannot invent data. This is why background scrub and repair must run
  promptly after the first failure.
- **A rebuild loop under load.** Repair traffic competes with user traffic. The
  bounded worker pool caps that competition, but a very degraded cluster may
  take a while to converge.
- **Quarantine growth.** Quarantined blocks accumulate until repaired; a
  persistently failing disk needs operator attention even though the cluster
  keeps serving.

## When NOT to Use This

- **A single-node deployment.** With no peers there are no survivors to fall back
  to; degraded read cannot help.
- **When read latency is sacrosanct above all.** The remote fallback adds a round
  trip. Systems with no latency budget may prefer to fail fast and repair
  out-of-band.
- **As a replacement for scrubbing.** Degraded read only triggers when a client
  happens to touch a bad block. Background scrub is what catches rot no one has
  read yet.

## References / Dig deeper

- Integrity and background scrubbing: [007](007-at-rest-integrity-and-gc.md).
- Domain-aware placement: [019](019-r1-failure-domain-placement.md).
- Write durability and quorum: [021](021-r3-write-durability-quorum.md).
- Seams architecture: [044](044-plugin-seam-architecture.md).
- Scatter-gather lease quorum: [017](017-scatter-gather-lease-quorum.md).
- Implementation: `src/storage/rebuild.go`, `src/storage/storage.go`.
- Spec: `openspec/changes/r2-self-heal-rebuild`; PR #953; tracking issue #928.
