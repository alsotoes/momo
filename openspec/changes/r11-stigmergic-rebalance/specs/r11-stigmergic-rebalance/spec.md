# R11 Stigmergic Rebalance — Auto-Rebalance via Local Rules

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1143

## Purpose

Enable automatic rebalancing on membership changes (join/leave/fail) and continuous homeostatic load balancing **without a central coordinator**. Each node runs independent local rules; global balance emerges from stigmergic interactions (ADAPTIVE_SYSTEMS.md: Stigmergy).

## Requirements

### Requirement 1: Membership Change Detection (Local)

#### Scenario: Node detects membership change via SWIM
**Given** SWIM gossip tracks `alive/suspect/offline` state
**When** a new peer joins or existing peer leaves/fails
**Then** each node independently detects this via gossip
**And** no central watcher or controller is involved

### Requirement 2: Stigmergic Shedding on Node Join

#### Scenario: New node joins cluster
**Given** a cluster of N nodes, new node N+1 joins
**When** existing nodes detect the new peer via SWIM gossip
**Then** each existing node independently:
  - Recomputes CRUSH placement for its local objects
  - Identifies objects where new peer is now a target replica
  - For each such object: sheds blob to new peer (stream + metadata update)
  - Continues serving reads (shedding is async, non-blocking)
**And** new peer stores received blobs, becomes valid replica
**And** no node coordinates the shed — each decides locally

### Requirement 3: Stigmergic Re-Replication on Node Leave

#### Scenario: Node leaves or fails
**Given** a cluster where peer P departs (SWIM marks OFFLINE)
**When** surviving replicas detect the departure
**Then** each surviving replica independently:
  - For each object where P was a replica:
    - Fetches blob from another surviving replica
    - Streams to new CRUSH target (computed on current topology)
    - Updates local metadata to reflect new replica set
**And** no coordinator assigns work — each surviving replica acts independently

### Requirement 4: Homeostatic Disk Pressure Shedding

#### Scenario: Node disk usage exceeds high water mark
**Given** `[rebalance] disk_high_water = 80`, `[rebalance] disk_low_water = 50`
**When** a node's local disk usage > 80%
**And** gossip reports a peer with disk usage < 50%
**Then** the node independently:
  - Identifies least-accessed blobs (via access stats)
  - Sheds cold blobs to the low-usage peer
  - Updates metadata to reflect new replica location
**And** shedding is rate-limited by `max_concurrent` and `max_bandwidth_mbps`

### Requirement 5: Homeostatic Replication Factor Maintenance

#### Scenario: Object under-replicated
**Given** an object's replica count < `replication_factor` (due to leave/failure)
**When** any surviving replica detects this (local metadata check)
**Then** the replica independently:
  - Selects new target via CRUSH on current topology
  - Fetches blob from another surviving replica
  - Streams to new target, updates metadata
**And** this runs in background goroutine, non-blocking

### Requirement 6: Failure Domain Spread

#### Scenario: Replicas concentrated in one failure domain
**Given** an object has >1 replica in the same failure domain
**When** the node detects this (via CRUSH placement + domain info)
**Then** the node independently:
  - Selects a replica in the overloaded domain
  - Migrates it to a node in a different domain (via CRUSH)
  - Updates metadata
**And** this runs during homeostatic check interval

### Requirement 7: Load Balancing via Request Redirection

#### Scenario: Node overloaded vs peer
**Given** `[rebalance] request_rate_ratio = 2.0`
**When** a node's request rate > 2x a peer's rate (via gossip metrics)
**And** the peer has the requested object (cache or local)
**Then** the node redirects the read to the peer (transparent proxy)
**And** the peer serves the read, caches locally
**And** this is the existing "Read From Any Node" proxy path

### Requirement 8: Degraded Read During Transition

#### Scenario: Read during rebalance
**Given** a read request for an object being shed/re-replicated
**When** current replicas don't have the blob (404)
**Then** the node tries old replicas (from pre-rebalance CRUSH placement)
**And** if found, serves from old replica and continues rebalance async
**And** availability is maintained throughout rebalance

### Requirement 9: Rate Limiting & Throttling

#### Scenario: Rebalance operations running
**Given** `[rebalance] max_concurrent = 4`, `max_bandwidth_mbps = 100`
**When** shedding or re-replicating
**Then** at most 4 concurrent operations per node
**And** bandwidth throttled to 100 Mbps per node
**And** foreground traffic (client reads/writes) always prioritized

## Configuration

```toml
[rebalance]
enabled = true
interval = 300              # seconds between homeostatic checks
max_concurrent = 4          # max concurrent shed/re-replicate per node
max_bandwidth_mbps = 100    # bandwidth throttling
disk_high_water = 80        # % disk usage to trigger shedding
disk_low_water = 50         # % disk usage to accept shed
request_rate_ratio = 2.0    # my_rate / peer_rate to trigger redirect
```

## Implementation Components

### 1. `src/rebalance/controller.go` — Local Rule Engine
```go
type RebalanceController struct {
    nodeID        int
    store         storage.Store
    peers         *p2p.PeerMap
    cmap          *common.ClusterMap
    config        RebalanceConfig
    shedSem       chan struct{}        // max_concurrent
    bandwidthLimiter *rate.Limiter    // max_bandwidth_mbps
}

func (rc *RebalanceController) Run(ctx context.Context) {
    ticker := time.NewTicker(rc.config.Interval)
    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            rc.runHomeostaticChecks()
        }
    }
}

func (rc *RebalanceController) runHomeostaticChecks() {
    rc.checkDiskPressure()
    rc.checkReplicationFactor()
    rc.checkFailureDomainSpread()
}

func (rc *RebalanceController) checkDiskPressure() {
    usage := rc.getLocalDiskUsage()
    if usage < rc.config.DiskHighWater {
        return
    }
    // Find peer with low usage via gossip
    for _, peer := range rc.peers.AliveByQuality() {
        if peer.DiskUsage < rc.config.DiskLowWater {
            rc.shedToPeer(peer)
            break
        }
    }
}
```

### 2. `src/rebalance/shed.go` — Blob Shedding Logic
```go
func (rc *RebalanceController) shedToPeer(peer *p2p.Peer) {
    blobs := rc.selectColdBlobs(10) // least accessed
    for _, blob := range blobs {
        select {
        case rc.shedSem <- struct{}{}: // acquire slot
            go func() {
                defer func() { <-rc.shedSem }()
                rc.streamBlobToPeer(blob, peer)
            }()
        default:
            return // backlogged, try next interval
        }
    }
}

func (rc *RebalanceController) streamBlobToPeer(hash string, peer *p2p.Peer) {
    reader, _, err := rc.store.Get(hash)
    if err != nil {
        return
    }
    defer reader.Close()
    
    // Stream via P2P transport with bandwidth limiting
    bwReader := rc.bandwidthLimiter.Reader(reader)
    comm, err := rc.dialPeer(peer)
    if err != nil {
        return
    }
    defer comm.Close()
    
    // Send shed metadata + blob
    // On success: update local metadata (remove replica), ack to peer
}
```

### 3. `src/rebalance/replicate.go` — Re-Replication on Leave
```go
func (rc *RebalanceController) OnPeerLeave(departedID int32) {
    // Scan local metadata for objects where departed was replica
    objects := rc.findObjectsWithReplica(departedID)
    for _, obj := range objects {
        go rc.reReplicateObject(obj)
    }
}

func (rc *RebalanceController) reReplicateObject(obj ObjectMeta) {
    // Find surviving replica
    survivor := rc.selectSurvivor(obj.MetadataReplicas, departedID)
    if survivor == -1 {
        return // no survivor, data lost
    }
    // Select new target via CRUSH on current topology
    target := rc.cmap.Placement(obj.Hash, rc.config.ReplicationFactor)
    targetID := rc.selectNewReplica(target, obj.MetadataReplicas)
    
    // Fetch from survivor, stream to target
    rc.streamBlobBetweenPeers(obj.Hash, survivor, targetID)
}
```

### 4. Integration with SWIM Events
```go
// In bootstrapP2P (server.go):
gossip.OnJoin(func(peer *p2p.Peer) {
    if rc != nil {
        rc.OnPeerJoin(peer.ID)
    }
})
gossip.OnLeave(func(peerID int32) {
    if rc != nil {
        rc.OnPeerLeave(peerID)
    }
})
```

## Consistency Model

- **Eventual consistency**: Rebalance operations are async; reads may see old replica set briefly
- **Degraded read**: If blob not found on new replicas, try old replicas (pre-rebalance placement)
- **No data loss**: Shed only completes after target confirms storage + metadata update
- **Idempotent**: Re-running shed/re-replicate is safe (metadata updates are idempotent)

## Failure Scenarios

### Shed In Progress, Target Fails
```
Node A sheds blob to Node B
  Node B stores blob, updates metadata
  Node B crashes before acking Node A
  
→ Node A retries shed (idempotent) when Node B recovers
→ Or Node A selects different peer next interval
```

### Re-Replication Source Fails
```
Node A re-replicates from Node C to Node D
  Node C crashes mid-stream
  
→ Node A selects different survivor, retries
→ If no survivor: data lost (but quorum writes should prevent this)
```

### Partition During Rebalance
```
Network partition splits cluster
  Each partition rebalances independently
  
→ When partition heals: CRUSH placement reconciles
→ Conflicts resolved via vector clocks (R6 metadata HA)
```

## Acceptance Criteria

- [ ] Node join → existing nodes shed to new node (local rule, no coordinator)
- [ ] Node leave → surviving replicas re-replicate (local rule, no coordinator)
- [ ] Disk > 80% + peer < 50% → shedding occurs (homeostasis)
- [ ] Under-replicated object → re-replication triggered (local detection)
- [ ] Replicas in same domain → migration to different domain
- [ ] Rebalance ops throttled by `max_concurrent` and `max_bandwidth_mbps`
- [ ] Reads available during rebalance (degraded read to old replicas)
- [ ] No central coordinator process or RPC
- [ ] Chaos test: random join/leave/fail → cluster converges to balanced state
- [ ] Benchstat: no foreground throughput regression during rebalance