# Change: Dynamic Replication Factor Control

**Related Issues:** https://github.com/alsotoes/momo/issues/165

## Why
To provide granular control over data durability and cluster performance, we must allow users to specify the number of replicas (e.g., 1, 3, 5). This moves Momo closer to a production-grade Object Storage system where durability is a policy choice.

## Technical Architecture

### 1. Global Configuration
- Parameter: `replication_factor` in `[global]` section of `momo.conf`.
- Default: 3.
- Logic: `n = min(GlobalReplicationFactor, ClusterNodeCount)`.

### 2. Strategy Integration
- **Chain Replication**: Data follows an ordered path (A -> B -> C) determined by the CRUSH placement list, stopping exactly after `n` hops.
- **Splay Replication**: The primary forwards data to `n-1` secondaries concurrently.
- **Primary-Splay**: The client connects to exactly `n` CRUSH-selected nodes and uploads to them directly.
- **No Replication**: Always overrides the factor to `1` (local storage only).

### 3. Resilience (Best-Effort)
If `GlobalReplicationFactor > ClusterNodeCount`, the system logs a high-visibility warning at boot and proceeds with all available nodes (**Degraded Mode**).

## What Changes
- `src/common/struct.go`: Add `ReplicationFactor int` to `ConfigurationGlobal`.
- `src/common/config.go`: Parse and validate the new parameter.
- `src/server/server.go`: Update placement calls and add boot-time validation.
- `src/client/client.go`: Refactor `Connect` to respect the replication factor limit.

## Verification
- **Test (Factor Bounding)**: Assert that setting `replication_factor = 2` on a 3-node cluster results in exactly 2 copies.
- **Test (Degraded Mode)**: Assert that setting `replication_factor = 5` on a 3-node cluster results in 3 copies and a "Degraded" warning.
- **Test (None Override)**: Verify that `ReplicationNone` still produces only 1 copy regardless of global factor.
