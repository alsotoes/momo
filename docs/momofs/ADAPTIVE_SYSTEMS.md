# Adaptive Systems Design — Learning from Nature

Momo's core features were inspired by natural systems (molecules, animals, adaptive systems). This document extends that philosophy to MomoFS, identifying biological principles that can make the system self-organizing, self-healing, and future-proof against use cases we can't yet predict.

The guiding question: **What can we learn from 4 billion years of evolution?**

---

## Core Principle: Stigmergy — Complex Behavior from Simple Local Rules

Termites build cathedral-complex nests without any blueprint or central planner. Each termite follows simple local rules: "if I sense pheromone here, deposit more, and build." The complex structure emerges from thousands of simple interactions.

**MomoFS application**: The cluster should behave like a termite colony — complex global behavior emerging from simple local rules on each node. No central coordinator. No global plan. Just local responses to local conditions.

```
Local rules (each node runs these independently):

  Rule 1 (homeostasis):    if my_disk > 80% → shed least-accessed blobs to neighbor with < 50%
  Rule 2 (healing):        if neighbor reports mismatch → repair from healthy replica
  Rule 3 (replication):    if my_replica_count < target → request re-replication
  Rule 4 (load balancing): if my_request_rate > 2× neighbor → redirect new requests to neighbor
  Rule 5 (caching):        if I fetched a blob remotely → cache it locally
  Rule 6 (apoptosis):      if my_disk_errors > threshold → self-decommission, migrate data, exit

  Global behavior that emerges:
    - Data automatically balanced across nodes (Rule 1)
    - Bitrot detected and repaired (Rule 2)
    - Replication factor maintained (Rule 3)
    - Load distributed evenly (Rule 4)
    - Hot data cached everywhere (Rule 5)
    - Failed nodes self-remove (Rule 6)

  No coordinator needed. No global state. Just local rules.
```

This is already partially present (P2P gossip, SWIM, polymorphic system). The design should extend this pattern — every new feature should be expressible as a local rule, not a global coordinator.

---

## Biological Principles → MomoFS Features

### 1. Immune System → Anomaly Detection & Auto-Quarantine

The adaptive immune system learns "normal" and detects "non-self." Memory cells remember past threats. The response is proportional — small threat, small response; large threat, systemic response.

**MomoFS application**: The cluster develops an "immune response" to problems.

```
Innate immunity (fast, generic — always active):
  - Disk error rate > threshold → mark disk degraded, stop writing to it
  - Node latency > 3× normal → reduce routing weight (SWIM already does this)
  - Checksum mismatch on read → mark replica corrupt, failover (planned: CRC32C)
  - Unexpected goroutine panic → log, recover (existing: Zero-Crash)

Adaptive immunity (slow, specific — learns over time):
  - Learn normal access patterns per tenant (request rate, file sizes, access times)
  - Detect anomalies: sudden spike in deletes from a tenant → possible attack or bug
  - Detect slow degradation: disk latency increasing over days → preemptive replacement
  - Memory cells: remember patterns of known-bad conditions (disk models that fail,
    config combinations that cause issues, network paths with high loss)
  - Response: quarantine affected data, alert, auto-repair if pattern is known

  "Memory B cell" = a BoltDB bucket storing learned patterns:
    bucketImmune = []byte("immune")
    key: pattern_hash → value: {pattern_type, severity, last_seen, auto_repair_action}
```

Effort: Medium. Pattern learning + anomaly detection. Phase 2-5.

### 2. Mycorrhizal Network → Cooperative Resource Sharing

Underground fungal networks connect trees. Trees with surplus resources share with trees in need. Warning signals propagate through the network. The forest is a connected organism, not isolated trees.

**MomoFS application**: Nodes share resources based on need, not just CRUSH placement.

```
Current: CRUSH deterministically places data. Node placement is static.

Mycorrhizal: Nodes dynamically share resources.

  Resource sharing:
    Node A: disk 95% full, receiving 5000 req/s → stressed
    Node B: disk 30% full, receiving 500 req/s → idle

    → Node A sheds cold data to Node B (stigmergy Rule 1)
    → Node B takes over some of Node A's read load (load balancing)
    → No central rebalancer — just local "I'm stressed, who can help?" gossip

  Warning propagation:
    Node C: detects disk SMART errors → warns neighbors
    → Neighbors stop placing new data on Node C
    → Neighbors start replicating Node C's data elsewhere
    → Node C's data is evacuated before it fails

  Nutrient routing (data gravity):
    Hot dataset on nodes [A, B, C] → compute jobs attracted to those nodes
    → "Data finds compute" (existing principle) extended to "compute finds data"
```

Effort: Medium. Gossip-based resource exchange. Phase 2-8.

### 3. Ant Colony Optimization → Adaptive Routing & Placement

Ants find optimal paths via pheromone trails. Short paths get more traffic → stronger pheromone → more ants. The colony converges on optimal paths without any ant knowing the global topology.

**MomoFS application**: Replica selection and read routing using pheromone-inspired reputation.

```
Current: Select replica with lowest RTT (static metric).

Ant colony: Select replica based on pheromone strength (learned over time).

  Pheromone deposit (successful read from Node X):
    pheromone[X] += reward * decay_factor
    (successful reads strengthen the trail)

  Pheromone evaporation (time-based decay):
    pheromone[X] *= 0.95 every second
    (old successes matter less — adapts to changing conditions)

  Pheromone penalty (failed read from Node X):
    pheromone[X] *= 0.1
    (failures rapidly weaken the trail)

  Replica selection:
    probability[X] = pheromone[X] / sum(all pheromones)
    → Weighted random selection (not greedy — allows exploration)

  Emergent behavior:
    - Fast, reliable nodes accumulate high pheromone → get more traffic
    - Slow or flaky nodes lose pheromone → get less traffic
    - New nodes start with neutral pheromone → get some traffic (exploration)
    - Network changes (new route, failed switch) → pheromone adapts within minutes
    - No static configuration of "preferred replicas"
```

Effort: Low. Per-node pheromone map + weighted selection. ~100 lines. Phase 1.

### 4. Apoptosis → Self-Decommissioning

Cells that are damaged or no longer needed self-destruct. This protects the organism — damaged cells don't spread problems. The cell signals neighbors, migrates its functions, then dies cleanly.

**MomoFS application**: Nodes that detect hardware degradation self-decommission before catastrophic failure.

```
Node self-monitoring (runs continuously):
  disk_errors_per_hour > 10      → "I am sick"
  memory_errors > threshold       → "I am sick"
  read_latency > 5× cluster_avg   → "I am degraded"
  goroutine_leak > 100K           → "I am leaking"

Self-decommission sequence (apoptosis):
  1. Mark self as "decommissioning" in P2P gossip
  2. Stop accepting new writes (redirect to other nodes)
  3. Continue serving reads (don't interrupt active clients)
  4. Migrate metadata shards to ring-adjacent nodes
  5. Re-replicate blob data to maintain replication factor
  6. Wait for in-flight requests to complete
  7. Log: "Node self-decommissioned due to disk errors"
  8. Exit process (K8s restarts with fresh state, or alerts ops)

  The cluster never sees a failure — the node leaves gracefully
  before it fails hard. Data is preserved. Clients are not interrupted.
```

Effort: Medium. Self-monitoring + graceful drain. Phase 2.

### 5. Quorum Sensing → Density-Dependent Feature Activation

Bacteria only execute certain behaviors when population density exceeds a threshold. Bioluminescence, biofilm formation, toxin production — all density-gated.

**MomoFS application**: Cluster features activate based on cluster state, not static config.

```
Current: All features are statically configured in momo.conf.

Quorum sensing: Features auto-activate when the cluster is ready.

  Auto-activation rules:
    if num_nodes >= 10 → enable erasure coding (needs many nodes for chunk placement)
    if num_nodes >= 3  → enable metadata replication (needs 3 for quorum)
    if total_data > 1TB → enable auto-tiering (not worth it for small datasets)
    if request_rate > 1000/s → enable aggressive caching (hot workload detected)
    if num_nodes > 100 → enable hierarchical gossip (flat gossip doesn't scale past ~1000)
    if avg_file_size > 100MB → enable striping (large files benefit from striping)
    if avg_file_size < 4KB → enable inline small files (DoM, from Lustre)
    if num_tenants > 1 → enable per-tenant quotas

  Benefits:
    - Zero-config for common cases — the cluster tunes itself
    - Features don't activate prematurely (no EC with 2 nodes)
    - Features activate when beneficial (caching when traffic is high)
    - Adapts to changing conditions (add nodes → more features activate)

  "Auto-config" bucket in BoltDB stores learned cluster characteristics:
    avg_file_size, request_rate, num_tenants, total_data, access_patterns
```

Effort: Medium. Cluster state monitoring + auto-activation logic. Phase 2-8.

### 6. Epigenetics → Same Code, Different Phenotype

Same genome, different behavior based on environment. A queen bee and worker bee have identical DNA — diet determines their fate. Organisms adapt within their lifetime without genetic change.

**MomoFS application**: Same MomoFS binary, radically different behavior based on environment.

```
Current: Behavior determined by momo.conf (static config).

Epigenetic: Behavior determined by environment + runtime adaptation.

  Environment detection (on startup):
    if running in K8s with GPU nodes → "I am an AI cluster"
      → enable vector embeddings, semantic search, GPU-accelerated hashing
    if running on NVMe with 100Gbps network → "I am an HPC cluster"
      → enable striping, large blob cache, parallel multi-node reads
    if running on small VMs with 1Gbps → "I am an edge cluster"
      → enable aggressive caching, limited storage, async sync to central
    if running across multiple regions → "I am a global cluster"
      → enable multi-region replication, data residency, GDPR compliance
    if running on spot instances → "I am ephemeral"
      → don't store primary data, cache only, evacuate on termination notice

  Runtime adaptation (continuous):
    if CPU > 80% for 5 min → reduce scrub frequency (existing polymorphic system)
    if disk < 10% free → stop accepting new data, shed cold data
    if network partition detected → enter "split-brain safe" mode (local quorum only)
    if traffic pattern changes (new tenant, new access pattern) → adapt caching strategy

  The "DNA" (Go binary) is identical everywhere.
  The "epigenetic markers" (environment + runtime state) determine behavior.
  No recompilation, no config changes — the system adapts.
```

Effort: Low-Medium. Environment detection + adaptive config. Phase 1-2.

### 7. Neuroplasticity → Access-Pattern-Driven Data Placement

The brain rewires itself based on experience. Frequently used neural pathways strengthen; unused ones weaken. Learning physically changes brain structure.

**MomoFS application**: Data placement adapts based on access patterns.

```
Current: Data placement is static (CRUSH computes once, data stays).

Neuroplastic: Data migrates based on how it's used.

  "Pathway strengthening" (frequently accessed data → better placement):
    File accessed 100×/day → migrate to NVMe tier (if available)
    File accessed 0× in 30 days → migrate to cold tier
    File always read with file B → co-locate on same node (reduce RPCs)

  "Pathway weakening" (unused data → cheaper placement):
    File not accessed in 90 days → reduce replication factor (save storage)
    File not accessed in 1 year → move to S3/Glacier backend
    File on expensive NVMe, never accessed → migrate to HDD

  "Co-firing" (files accessed together → co-located):
    Track access correlation: if GET /a.csv is followed by GET /b.csv 80% of the time
    → Place a.csv and b.csv on the same node (or same rack)
    → Next GET /a.csv + GET /b.csv: both local, zero remote RPCs

  Implementation:
    access_stats bucket (already planned in Phase 5):
      key: hash → {access_count, last_access, co_accessed_with[]}
    Background "neuroplasticity" goroutine:
      periodically reviews access_stats → migrates data → updates placement
```

Effort: Medium. Access tracking + migration logic. Phase 5-8.

### 8. Circadian Rhythms → Time-Aware Resource Management

Biological processes follow cycles. Metabolism, activity, resource allocation change predictably over time.

**MomoFS application**: The cluster adapts to temporal patterns.

```
Current: Constant behavior regardless of time.

Circadian: Time-aware adaptation.

  Learned temporal patterns:
    - Business hours (9am-5pm): high read traffic → pre-warm caches at 8:30am
    - Batch jobs (2am-4am): high write traffic → increase replication factor at 1:30am
    - Weekend: low traffic → schedule deep scrub, reduce cache size
    - Month-end: spike in deletes (compliance) → increase GC frequency

  Implementation:
    temporal_stats bucket:
      key: hour_of_day → {avg_read_rate, avg_write_rate, avg_delete_rate}
    Scheduler uses temporal stats to pre-emptively adapt:
      - Pre-warm cache before predicted traffic spike
      - Schedule scrub during predicted low traffic
      - Adjust replication factor before predicted write burst
      - Pre-replicate data that will be needed (predictive placement)

  No static cron jobs — the system learns temporal patterns and adapts.
```

Effort: Medium. Temporal pattern learning + predictive scheduling. Phase 5-8.

### 9. Symbiosis → Heterogeneous Node Clusters

Different organisms live together in mutually beneficial relationships. Each provides what the other lacks. Lichens = fungus + algae. The whole is greater than either part.

**MomoFS application**: Heterogeneous nodes in the same cluster, each contributing different capabilities.

```
Current: All nodes are assumed homogeneous (same storage, same network).

Symbiotic: Nodes have different capabilities and complement each other.

  Node types in one cluster:
    "Hot nodes" (NVMe, 100Gbps): serve hot data, metadata shards, handle reads
    "Warm nodes" (SSD, 10Gbps): serve warm data, handle writes
    "Cold nodes" (HDD, 1Gbps): store cold data, handle scrub/repair
    "AI nodes" (GPU + NVMe): compute vector embeddings, semantic search
    "Edge nodes" (small disk, intermittent): cache + sync to central
    "Archive nodes" (S3 backend): infinite storage, slow access, cheap

  CRUSH placement respects node type:
    New hot data → place on hot nodes
    Aging data → migrate to warm → cold → archive
    AI embedding request → route to AI node
    Edge sync → edge node syncs to nearest hot node

  The cluster is a symbiotic organism — each node type contributes what it does best.
  No need for separate clusters for hot/cold/AI/edge — one cluster, heterogeneous nodes.
```

Effort: Medium. Node capability tags + CRUSH rules. Phase 8.

### 10. Homeostasis → Cluster Equilibrium

Blood pH stays at 7.4 ± 0.05 despite everything you eat, drink, or do. Multiple feedback loops maintain equilibrium. Perturbations trigger corrective responses.

**MomoFS application**: The cluster maintains equilibrium through feedback loops.

```
Homeostatic variables and feedback loops:

  Disk utilization per node:
    target: all nodes within 60-80% full
    sensor: local disk stats
    response: if > 80% → shed to neighbor; if < 50% → accept from neighbor

  Request latency:
    target: p99 < 5ms
    sensor: request timing
    response: if p99 > 10ms → enable more caching, add replicas, redirect traffic

  Replication factor:
    target: 3 copies per object (configurable)
    sensor: replica count per object
    response: if < 3 → re-replicate; if > 3 → remove excess

  Cluster size:
    target: enough nodes to handle load
    sensor: CPU/memory utilization across cluster
    response: if all nodes > 80% CPU → trigger auto-scale (K8s HPA)
              if all nodes < 20% CPU → trigger auto-scale-down

  Metadata shard balance:
    target: each node owns ~N/shards shards
    sensor: shard ownership count
    response: if imbalanced → ring rebalance (consistent hashing: minimal movement)

  Each loop is independent, runs continuously, and corrects deviations.
  The cluster "feels" perturbations and responds — like a living organism.
```

Effort: Low-Medium. Feedback loops built on existing metrics. Phase 2.

### 11. Swarm Intelligence → Coherent Collective Movement

Flocks of birds move as one without a leader. Each bird follows three rules: separation (don't crowd), alignment (match neighbor velocity), cohesion (stay near group).

**MomoFS application**: Cluster-wide changes propagate coherently without a leader.

```
Current: Replication mode changes broadcast via ChangeReplication endpoint.

Swarm: All cluster-wide behavior changes via local flocking rules.

  Replication mode change:
    Node A detects high CPU → switches to Splay (lower overhead)
    Node A broadcasts mode change via gossip
    Neighbors align: "Node A switched to Splay, I should too if my CPU is high"
    Cohesion: "Most nodes are now Splay, I should align even if my CPU is normal"
    Separation: "I'm different from neighbors if my workload is different"

  Data migration (new node joins):
    New node appears → neighbors sense new capacity
    Closest neighbors (by ring position) shed some shards to new node
    Each neighbor decides independently how much to shed
    Collective result: new node reaches equilibrium with neighbors
    No central "rebalance" command — just local shedding

  Cache warming:
    Node A caches a hot blob → neighbors sense (via gossip) that A has it
    Neighbors fetch from A (cooperative caching) → blob spreads
    Eventually all nodes near the access pattern have the blob cached
    When access pattern changes, cache entries expire → blob "fades" from distant nodes
```

Effort: Low. Built on existing P2P gossip. Phase 1-2.

---

## Future-Proofing: Use Cases We Can't Predict

### 12. Pluggable Everything — Evolution's "Mutation" Mechanism

Evolution works because mutations are possible. Most are neutral or harmful, but occasionally one is beneficial and spreads. The system must allow mutations (new features) without redesign.

**MomoFS application**: Every major component must be pluggable.

```
Already pluggable:
  - BlobStore interface (local, S3, raw, NFS) → can add new backends
  - Communicator interface (TCP, QUIC, S3) → can add new transports
  - Replication strategies (Chain, Splay, Primary-Splay) → can add new strategies

Should be pluggable:
  - Placement algorithm (CRUSH, consistent hashing) → can add new algorithms
  - Metadata store (BoltDB) → can add RocksDB, BadgerDB, custom
  - Consensus protocol (lease) → can add Raft, Paxos
  - Failure detector (SWIM) → can add phi-accrual, hybrid logical clocks
  - Search index (BoltDB cursor) → can add HNSW, inverted index, vector index
  - Encryption (none) → can add AES-GCM, post-quantum (lattice-based)
  - Compression (none) → can add zstd, lz4, zstd with dictionary

  Plugin interface:
    type Plugin interface {
        Name() string
        Init(config map[string]interface{}) error
        Close() error
    }

  Load plugins at startup:
    momo --plugin=/path/to/rocksdb.so --plugin=/path/to/hnsw.so

  Future use case: "We need post-quantum encryption"
    → Write a plugin, load it. No core code changes.

  Future use case: "We need a new placement algorithm for rack-aware GPU clusters"
    → Write a plugin, load it. No core code changes.
```

Effort: Medium. Define plugin interface, refactor internals. Phase 1-8.

### 13. Data Gravity — Attracting Compute to Data

As data accumulates, it "weighs" more — attracts compute, applications, and more data. Moving large datasets to compute is expensive; moving compute to data is cheap.

**MomoFS application**: Support compute-to-data patterns.

```
Current: Data is served to clients (data moves to compute).

Data gravity: Compute can be sent to the data.

  "Serverless on MomoFS":
    POST /report.pdf?exec=python3&script=analyze.py
    → Node with the blob runs the script locally
    → Returns result (not the data)
    → No data transfer over network

  "MapReduce on MomoFS":
    map(function, prefix="/data/logs/")
    → Each node with logs/ data runs the function locally
    → Results aggregated by the requesting node
    → Data never moves — compute moves to where data lives

  This is like cells sending mRNA to where proteins are needed,
    rather than moving proteins around the cell.
```

Effort: High. Server-side execution framework. Phase 8+ (future).

### 14. Federated Clusters — Symbiosis Between Clusters

Multiple independent MomoFS clusters that cooperate selectively. Like a federation of organisms — each independent, but sharing resources when beneficial.

**MomoFS application**: Cross-cluster data sharing without merging.

```
Cluster A (company's main cluster)    Cluster B (partner organization)
  5 nodes, 100TB                        3 nodes, 50TB

  Federation:
    - Cluster A shares bucket "public-datasets" with Cluster B
    - Cluster B can read from A's "public-datasets" via S3 API
    - No data replication (unless explicitly configured)
    - No shared metadata — each cluster is independent
    - Access control: per-cluster auth, per-bucket sharing policy

  Use cases:
    - Multi-cloud (AWS + GCP + on-prem, each a MomoFS cluster)
    - Partner data sharing (share specific buckets, keep rest private)
    - Edge-to-core (edge clusters sync to core, but core doesn't sync to edge)
    - Disaster recovery (async replication between federated clusters)

  Like mycorrhizal networks between forests — connected but independent.
```

Effort: Medium. Cross-cluster S3 proxy + auth. Phase 8.

### 15. Temporal Data — Time-Aware Storage

Many future use cases involve temporal data: IoT sensor streams, event logs, audit trails, time-series. Data has time semantics that plain object storage doesn't understand.

**MomoFS application**: Optional temporal awareness.

```
Temporal bucket mode:
  PUT /sensors/temp-001?timestamp=2026-07-29T10:00:00Z
  → Stored with timestamp in temporal index
  → Auto-expire after retention period (TTL)

  GET /sensors/temp-001?time=2026-07-29T09:00:00Z
  → Time-travel query: return value as of that time

  GET /sensors/temp-001?range=2026-07-29T00:00:00Z,2026-07-29T23:59:59Z
  → Range query: all values in time range

  Implementation:
    BoltDB temporal bucket: key = name + timestamp (sorted)
    Cursor scan for time range queries
    TTL-based GC (existing tombstone mechanism, extended)
```

Effort: Medium. Temporal indexing + time-travel. Phase 6-8.

---

## Implementation Priority

### Phase 1-2 (Foundation + Self-Healing)

| # | Feature | Bio Inspiration | Effort |
|---|---------|----------------|--------|
| 3 | Pheromone-based replica selection | Ant colony | ~100 lines |
| 10 | Homeostatic feedback loops | Homeostasis | Low |
| 4 | Apoptosis (self-decommissioning) | Apoptosis | Medium |
| 6 | Epigenetic environment detection | Epigenetics | Low |
| 11 | Swarm-based mode propagation | Swarm intelligence | Low (extends existing) |

### Phase 2-5 (Intelligence)

| # | Feature | Bio Inspiration | Effort |
|---|---------|----------------|--------|
| 1 | Immune system anomaly detection | Immune system | Medium |
| 2 | Mycorrhizal resource sharing | Mycorrhizae | Medium |
| 5 | Quorum sensing (auto-features) | Quorum sensing | Medium |
| 7 | Neuroplastic data placement | Neuroplasticity | Medium |

### Phase 5-8 (Future-Proofing)

| # | Feature | Bio Inspiration | Effort |
|---|---------|----------------|--------|
| 8 | Circadian temporal adaptation | Circadian | Medium |
| 9 | Symbiotic heterogeneous nodes | Symbiosis | Medium |
| 12 | Pluggable everything | Evolution/mutation | Medium |
| 14 | Federated clusters | Symbiosis (between clusters) | Medium |
| 15 | Temporal data | Biological clocks | Medium |

### Future (Phase 8+)

| # | Feature | Bio Inspiration | Effort |
|---|---------|----------------|--------|
| 13 | Data gravity (compute-to-data) | Cell signaling | High |

---

## Design Philosophy

1. **Local rules, global emergence** — Every feature should be expressible as a local node behavior, not a central coordinator. (Stigmergy)
2. **Adapt, don't configure** — The system should tune itself based on environment and workload. (Epigenetics)
3. **Fail gracefully, heal automatically** — Problems are detected early, contained locally, and repaired without human intervention. (Immune system + apoptosis)
4. **Mutate and select** — New features can be added as plugins without redesign. The useful ones persist. (Evolution)
5. **Cooperate, don't compete** — Nodes share resources based on need. The cluster is an organism, not a collection of individuals. (Mycorrhizae + symbiosis)
6. **Learn from experience** — Access patterns, temporal patterns, and failure patterns shape future behavior. (Neuroplasticity + immune memory)
7. **Diversity is strength** — Heterogeneous nodes, multiple backends, pluggable components. Monoculture is fragile. (Symbiosis + biodiversity)
