# Change: Content-Addressable Storage (CAS) 2.0 with CRUSH & Bbolt

**Related Issues:** https://github.com/alsotoes/momo/issues/151, https://github.com/alsotoes/momo/issues/163

## Why
The current storage model is name-based and relies on a fixed primary node (Node 0). To transform Momo into a truly scalable, high-performance **Object Storage system**, we must:
1.  **Deduplicate Data**: Identify files by their content hash (SHA-256) to save space and replication bandwidth.
2.  **Eliminate Central Bottlenecks**: Move from a "Node 0 is Primary" model to a "Deterministic Placement" model.
3.  **Harden Metadata**: Use a robust, ACID-compliant database for metadata rather than simple directory structures.

## Technical Architecture

### 1. Algorithmic Placement (CRUSH-lite)
Instead of a central directory, Momo will use a simplified Go implementation of the **CRUSH** (Controlled Replication Under Scalable Hashing) algorithm, originally conceived by **Sage Weil**.
- **Cluster Map**: Every node and client shares a small map of the cluster topology and node weights.
- **Deterministic Routing**: For any given object hash $H$, the placement function $P(H, Map, ReplicationFactor)$ returns an ordered list of nodes.
- **Balanced Primaries**: This ensures that every node in the cluster acts as a "Primary" for a random subset of the data, perfectly balancing the write load.

### 2. Partitioned Metadata (Bbolt)
Each node maintains a local **Bbolt** instance (`momo.db`) in its data directory.
- **Bucket `Objects`**: Maps `ContentHash` -> `{Size, RefCount, PhysicalPath}`.
- **Bucket `Namespace`**: Maps `HumanName` -> `ContentHash`.
- **Partitioning**: Metadata is partitioned across the cluster using the same CRUSH algorithm as the data blobs. A client knows exactly which node holds the metadata for `file_x.zip` by hashing the name.

### 3. Storage Hierarchy (Blob Store)
Data blobs are stored in a tiered directory structure derived from the hash to ensure optimal filesystem performance:
- `data/blobs/ab/cd/ef/abcdef123...`

## What Changes
- **`src/storage`**: New package implementing the `Store` interface with Bbolt and the CRUSH-lite engine.
- **Handshake Evolution**: Update the `Communicator` handshake so clients send the **Object Hash** first.
- **Deduplication Logic**: Servers will check their Bbolt index and reply with `ALREADY_HAVE` if the hash exists, skipping redundant transfers.
- **Replication Core**: Refactor the replication loop to use the CRUSH-calculated node list instead of the fixed `daemons` slice order.

## Impact
- **Performance**: High O(1) lookups and balanced cluster-wide throughput.
- **Reliability**: Zero-Crash compliant via Bbolt's ACID transactions and atomic filesystem renames.
- **Scaling**: Adding a node only requires updating the Cluster Map; data migration is handled algorithmically.

## Verification
- Assert that identical files are deduplicated into a single physical blob.
- Verify that write load is evenly distributed across multiple "Primary" nodes.
- Stress test Bbolt metadata lookups under high concurrency.
