# Spec: R10 Version-Record Replication

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1233

## Purpose

Make version listing, version-addressed reads/deletes, and object-lock state
consistent across every node that stores an object's replicas, instead of only
on the node that served the write.

## Requirements

### R10-VR1: Idempotent record application

**Requirement:** A node MUST be able to apply a version record created
elsewhere without changing its identity or corrupting the local chain.

**Scenario: Re-apply**

Given a node already holds version `v1`,
When the same record arrives again,
Then the apply is a no-op.

**Scenario: Stale latest**

Given the local chain's newest record has timestamp 100,
When a replicated record with timestamp 50 marked latest arrives,
Then the record is stored without the latest flag and the local latest stays.

### R10-VR2: Replication on local write

**Requirement:** Creating a version or delete marker locally MUST replicate the
record to the object's replica set when replication is enabled.

**Scenario: Fan-out**

Given a 3-node cluster with replication factor 2 and self = node 0,
When node 0 records a version for key `k`,
Then every non-self node in the object's placement receives `MsgReplicateVersion`
carrying the bucket, key, record, and versioning status.

**Scenario: Single node**

Given replication factor 1,
When a version is recorded,
Then no replication is attempted.

**Scenario: Replicated records do not re-replicate**

Given a record applied through the replication RPC,
When the apply completes,
Then the local replication hook is not fired.

### R10-VR3: Best-effort delivery

**Requirement:** Replication failures MUST NOT fail the write.

**Scenario: Peer unreachable**

Given a replica peer is down,
When a version is recorded,
Then the local write succeeds and an AUDIT log line records the failed peer.

**Scenario: Request timeout**

Given a peer does not acknowledge within the timeout,
When replication is attempted,
Then the call returns a timeout error that the fan-out logs.
