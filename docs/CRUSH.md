# CRUSH-lite: Deterministic Object Placement via Weighted Rendezvous Hashing

This document details the architecture, design goals, and performance optimizations of Momo's **CRUSH-lite** algorithm, contrasting it with Sage Weil's original **CRUSH (Controlled Replication Under Scalable Hashing)** algorithm implemented in Ceph's RADOS (2006).

---

## 1. Context & Architectural Rationale

In standard distributed storage architectures, clients locate data by querying a centralized metadata index server (e.g., HDFS NameNode). This centralized directory represents a single point of failure (Splay vulnerability) and becomes a severe tail-latency bottleneck under heavy parallel client traffic.

Like Ceph, Momo completely eliminates the centralized metadata server. Clients and server daemons calculate the location of any file object **algorithmically** using its content-addressable SHA-256 hash. By executing the placement algorithm locally, Momo achieves:
- **Infinite horizontal scalability**: Nodes can be added without bloating a central index.
- **Zero-metadata lookup overhead**: Node discovery completes in sub-microsecond CPU cycles.
- **High fault tolerance**: Nodes independently arrive at the identical placement decision without cross-network synchronization.

---

## 2. Comparing RADOS CRUSH vs. Momo's CRUSH-lite

While Momo adopts the core philosophy of Sage Weil's CRUSH (rule-based, content-addressable, deterministic mapping), the original RADOS implementation was stripped and optimized to create **CRUSH-lite** to meet Momo's strict performance criteria.

| Architectural Dimension | Ceph RADOS CRUSH (Sage Weil, 2006) | Momo CRUSH-lite |
| :--- | :--- | :--- |
| **Topology & Hierarchy** | **Complex & Deep:** Datacenter $\rightarrow$ Row $\rightarrow$ Rack $\rightarrow$ Chassis $\rightarrow$ Host $\rightarrow$ OSD (Leaf). | **Flat & Compact:** Multi-region awareness mapped directly over flat virtual node rings. |
| **Replication Selection** | Recursive tree/list/straw bucket backtracking on collision or failure. | Deterministic, single-pass flat hashing with linear fallback probing. |
| **Mathematical Precision** | Weight-based float64 division and logarithmic scaling. | SHA-256 hashing + Weighted Rendezvous Hashing (WRH) with float64 scoring. |
| **CPU Speed** | Microsecond scale (bound by tree traversal and backtrack recursion). | **Sub-microsecond scale (~400 ns)** (bound by SHA-256 computation and sort). |
| **Memory Allocation** | Heavy heap-allocated node tree traversals and array slices. | **Minimal allocation** (score slice + result slice; stack-allocated hash buffers). |

---

## 3. The Mathematical Trade-offs & Optimization

### Why RADOS CRUSH is Overkill for Momo
RADOS CRUSH is designed to model massive, heterogeneous physical failure domains. If a rack loses power, RADOS CRUSH recalculates replication targets to ensure copies are splayed across different rows. This requires:
1. Building and parsing complex hierarchical parent-child pointer trees.
2. Backtracking recursive loops when a node collision occurs.
3. Heavy float math to calculate weighted probability distribution.

In a high-throughput playground, these operations cause severe **CPU cache misses** and trigger **heap escapes** (memory allocations), which in turn invoke Go's Garbage Collector (GC), introducing unpredictable tail-latency spikes (stop-the-world pauses).

R1 (#929) adds a middle ground: an optional **flat** `failure_domain` label per node — no hierarchy, no buckets, no backtracking — that constrains replica spread across independent failure units while preserving the single-pass, cache-friendly design (see §3, step 4).

### How CRUSH-lite Solves This (⚡ Bolt Standard)
Momo's `CRUSH-lite` simplifies the topology to a flat, region-aware ring and uses Weighted Rendezvous Hashing (WRH) with SHA-256 for deterministic, load-balanced placement.

1. **Deterministic Hashing:**
   For each candidate node, a SHA-256 digest is computed over the concatenation of the object hash and the node ID:
   $$H = \text{SHA-256}(\text{objectHash} \parallel \text{nodeID})$$
   The score is derived by folding a 52-bit mantissa from the digest (top 32 bits at offset 0 plus the bottom 20 bits at offset 28) and dividing by $2^{52}$, yielding a float64 in $[0, 1)$:
   $$\text{score} = \frac{\text{top32}\; \ll 20 \mid \text{low20}}{2^{52}}$$
   (fix #647: a full-uint64 → `/MaxUint64` conversion discards the digest's low ~11 bits and biases placement; the 52-bit mantissa fold keeps every mantissa bit meaningful with no precision loss.)
2. **Weighted Rendezvous Hashing (WRH):**
   Each node's final placement score incorporates its weight using the WRH formula:
   $$\text{finalScore} = -\frac{\text{weight}}{\ln(\text{score})}$$
   This provides mathematically optimal load balancing for heterogeneous nodes — heavier nodes receive proportionally more objects.
3. **Replication Peer Selection:**
   All nodes are sorted by `finalScore` descending. The top $R$ nodes are selected as the placement targets:
   $$\text{placement} = \text{sort}_{\text{desc}}(\text{nodes}, \text{finalScore})[:R]$$
   This ensures deterministic, load-balanced replication with minimal data movement when nodes are added or removed.
4. **Failure-Domain Spread (R1, #929):**
   When at least one node declares a `failure_domain` (per `[daemon.N]`), selection maximizes the number of **distinct failure domains** in the replica set, tie-broken by descending `finalScore`. Because scores are already sorted descending, a single greedy pass — taking the highest-scoring node of each unused domain first, then filling any remaining slots by score — is equivalent to brute-force optimization over replica sets (cluster sizes are small; no hierarchy or buckets). If $R$ exceeds the number of distinct domains, placement still returns $R$ replicas (sharing domains) and logs a degraded-mode warning, consistent with the replication-factor cap warning. Nodes without a `failure_domain` share one default ("unclassified") domain; when no node declares a domain, the legacy top-$R$ path runs unchanged with zero added cost.

---

## 4. Performance Proof & Implementation

Our optimized `CRUSH-lite` implementation reduces heap allocations by:
- Using stack-allocated 8-byte buffer for node ID encoding (`var idBuf [8]byte`, `binary.LittleEndian.PutUint64`).
- Using stack-allocated 32-byte buffer for SHA-256 digest (`var sumBuf [sha256.Size]byte`), avoiding `h.Sum(nil)` heap allocation.
- Avoiding reflection-heavy `binary.Write` in favor of direct `binary.LittleEndian` calls.

### Live Performance Metrics Comparison
The micro-benchmarks demonstrate the performance of the WRH + SHA-256 design:

*   **`BenchmarkCrushOriginal` (~400 ns/op, 164 B/op, 3 allocs/op):** Uses standard reflection, `binary.Write`, and `h.Sum(nil)` heap allocation.
*   **`BenchmarkCrushOptimized` (~300 ns/op, 0 B/op, 0 allocs/op):** Uses stack-allocated buffers for node ID and SHA-256 digest, eliminating heap escapes in the hot path.

> **Note:** The micro-benchmarks above (`BenchmarkCrushOriginal`/`BenchmarkCrushOptimized` in `bench_crush_test.go`) measure the pre-#647 scoring path (full-uint64 `/MaxUint64` normalization); the shipping `Placement` uses the 52-bit mantissa fold in `hashToScoreValue` (`crush.go`), so benchmark figures are indicative of the allocation profile rather than an exact measurement of the shipping placement path.

By stripping out Ceph's heavy hierarchical backtrack recursion, Momo's `CRUSH-lite` executes in sub-microsecond time with minimal GC pressure, guaranteeing predictable latencies during intensive S3 gateway streams.
