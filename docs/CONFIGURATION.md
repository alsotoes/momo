# Configuring Momo

This document provides a comprehensive guide to all the configuration options available in the `momo.conf` file. A valid configuration file is required for the Momo application to start.

## File Format

The configuration file uses a standard INI-style format. The parser is flexible and supports the following features:

-   **Sections:** Configuration keys are grouped into sections, denoted by `[section_name]`.
-   **Key-Value Pairs:** Each setting is defined as a `key = value` pair.
-   **Comments:** Lines beginning with `#` or `;` are treated as comments and are ignored.

## Configuration Sections

### [global]

This section contains cluster-wide settings that affect all daemons.

-   **`auth_token`**
    -   **Description:** A shared secret token used for authentication between clients and servers. All nodes in the cluster must share the same token. For S3-compatible protocols, this token is used as the AWS access key ID. Tokens longer than 64 bytes are rejected with `EINVAL` at startup.
    -   **Type:** String (exactly 64 bytes when null-padded; max 64 bytes)
    -   **Default:** None (required)
    -   **Example:** `a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6` <!-- notsecret -->

-   **`auth_backoff_delay`**
    -   **Description:** Base delay (in milliseconds) for the adaptive failed-auth backoff (issue #821). When **0** (default), auth throttling is disabled and authentication behaves exactly as before. When **> 0**, consecutive failed challenge-response handshakes from a single source grow the per-source rejection delay exponentially (factor 2, capped at 8s), and a source exceeding 5 consecutive failures is temporarily locked out. A successful authentication releases the source immediately. Applies to the `momo-tcp` and `momo-quic` data handshakes and the change-replication control channel.
    -   **Type:** Integer (milliseconds, `>= 0`)
    -   **Default:** `0` (disabled)

-   **`debug`**
    -   **Description:** When set to `true`, enables verbose debug logging for all daemons in the cluster.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** None (required)

- **`replication_order`**
    -   **Description:** A comma-separated list of integers that defines the sequence of replication strategies the polymorphic system can cycle through. The order determines the path of escalation and de-escalation based on system load.
    -   **Type:** Comma-separated list of integers (e.g., `3,2,1`)
    -   **Possible Values:** Each integer corresponds to a replication strategy:
        -   `1`: Chain Replication (0 -> 1 -> 2)
        -   `2`: Splay Replication (0 -> 1, 0 -> 2)
        -   `3`: Primary-Splay Replication (Client -> 0, 1, 2)
    -   **Default:** None (required)
    -   **Note:** Mode `0` (No Replication) is used internally by the cluster to signal the end of a replication sequence and should not be included in the configuration.


-   **`replication_factor`**
    -   **Description:** Defines the target number of physical copies (replicas) to maintain for every object in the cluster. Momo uses the CRUSH-lite algorithm to select this many distinct nodes for storage.
    -   **Type:** Integer
    -   **Default:** `3`
    -   **Logic:** If the cluster contains fewer than `replication_factor` nodes, the system will store as many copies as possible and log a warning (**Degraded Mode**).

-   **`minimum_durability_factor`**
    -   **Description:** The minimum achievable replica copy count the metrics-driven controller may select under load (issue #822). When **0** (default), the durability floor is disabled and the controller behaves exactly as before. When **> 0**, the controller refuses to automatically degrade to a replication mode whose achievable replica count (computed as `min(replication_factor, number_of_daemons)` for replicated modes, and `1` for `ReplicationNone`) falls below this floor — it holds the current higher-durability mode and logs the refusal instead of silently losing durability. Operator-driven mode changes via the change-replication control channel are not affected.
    -   **Type:** Integer (`>= 0`, and `<= replication_factor` when enabled)
    -   **Default:** `0` (disabled)

-   **`client_side_replication_modes`**
    -   **Description:** A comma-separated list of replication mode IDs that require a momo-aware client. External S3 clients (e.g., `aws-cli`) cannot perform these modes, so when such a client connects, the server downgrades to the next server-side mode in `replication_order` per connection.
    -   **Type:** Comma-separated list of integers
    -   **Default:** `3` (Primary-Splay)
    -   **Example:** `client_side_replication_modes=3`

-   **`polymorphic_system`**
    -   **Description:** When set to `true`, enables the polymorphic engine on the primary server (daemon 0), allowing the cluster to change replication strategies dynamically based on system load.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** None (required)

-   **`protocol`**
    -   **Description:** Defines the transport layer used for all intra-cluster and client-server communication.
    -   **Type:** String
    -   **Possible Values:**
        -   `momo-tcp`: High-performance raw TCP transport.
        -   `momo-quic`: Modern encrypted transport running over UDP utilizing TLS 1.3 and QUIC streams.
        -   `s3-tcp`: AWS S3-compatible REST API mapping over standard TCP.
        -   `s3-quic`: AWS S3-compatible REST API mapping over secure QUIC streams.
    -   **Default:** `momo-tcp` (if omitted, falls back to `momo-tcp` with a warning log)

-   **`tls_cert`**
    -   **Description:** Path to a PEM-encoded TLS certificate for the TCP-based protocols (`momo-tcp`, `s3-tcp`). When set together with `tls_key`, TCP connections are wrapped in TLS 1.2+ before any application data is exchanged. When empty, TCP connections use plaintext (backward compatible).
    -   **Type:** String (file path)
    -   **Default:** (none — plaintext TCP)

-   **`tls_key`**
    -   **Description:** Path to the PEM-encoded private key that pairs with `tls_cert` for TCP-based protocols.
    -   **Type:** String (file path)
    -   **Default:** (none — plaintext TCP)

-   **`ca_cert`**
    -   **Description:** Path to a PEM-encoded CA certificate used to verify QUIC peer certificates. QUIC peer verification defaults to strict: either `ca_cert` must be configured or `tls_insecure = true` must be explicitly set. This prevents accidental Man-in-the-Middle vulnerabilities.
    -   **Type:** String (file path)
    -   **Default:** (none — QUIC requires `tls_insecure = true` or a CA cert)

-   **`tls_insecure`**
    -   **Description:** When set to `true`, skips TLS certificate verification for QUIC and TCP TLS. Must be explicitly opted into; **not recommended for production**. When `false` (default), QUIC requires a valid `ca_cert` unless the peer is trusted by the system pool.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** `false`

-   **`encryption_enabled`**
    -   **Description:** When set to `true`, enables end-to-end (E2EE) content encryption. All content is encrypted with AES-GCM-256 before storage/transfer, and the server becomes zero-knowledge — it stores ciphertext and opaque metadata without ever seeing plaintext. Requires a valid `encryption_key`.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** `false`

-   **`encryption_key`**
    -   **Description:** The master encryption key used for E2EE, a 64-character hex-encoded 256-bit key. Required when `encryption_enabled = true`. Keys that are not 64 hex characters are rejected with `EINVAL` at startup.
    -   **Type:** String (64 hex characters)
    -   **Default:** (none — required when encryption is enabled)

-   **`encryption_tenant`**
    -   **Description:** The tenant identifier used for per-tenant key derivation via HKDF-SHA256. The master key is never used directly for content encryption; a tenant-specific key is derived as `HKDF-SHA256(masterKey, salt=nil, info=tenantID)`. Defaults to `"default"` when empty.
    -   **Type:** String
    -   **Default:** `default`

-   **`oprf_enabled`**
    -   **Description:** Enables **confidential dedup via a threshold Oblivious PRF (OPRF)**. When enabled, the client derives each file's content key from the plaintext dedup tag (`SHA-256(plaintext)`) through a threshold OPRF evaluated over a quorum of daemons. Identical plaintexts deduplicate to a single blob under `H(plaintext)` across tenants, while no single daemon can derive the content key offline because the OPRF secret is Shamir-split across the cluster. Content is still encrypted with AES-GCM-256 end-to-end. Requires `encryption_enabled = true`, an `oprf_share` (and optionally `oprf_share_index`) on **every** `[daemon.N]` section, and `[p2p]` enabled when `oprf_threshold > 1`. The operation fails closed (no convergent fallback) when fewer than `oprf_threshold` daemons respond.
    -   **Protocol availability:** OPRF confidential dedup is primarily a **native** capability: the native protocols (`momo-tcp`, `momo-quic`) perform the binary `ModeOPRFEval` (`'O'`) handshake, and a momo client with `oprf_enabled = true` dials a native transport for evaluation. The S3 gateway (`s3-tcp`, `s3-quic`) additionally exposes an RPC mirror of the evaluation at `POST /?momo-oprf-eval` (same wire layout, driven by `S3Communicator.SendOPRFEval`) so a config that sets `protocol` to an S3 value still functions; this mirror is **not a designed parity surface** — standard S3-ecosystem clients cannot perform OPRF. Requires `[p2p]` enabled when `oprf_threshold > 1` (peers evaluate their Shamir shares via P2P). A client that cannot perform OPRF evaluation (e.g. a stock `aws-cli` upload with `oprf_enabled`) still fails closed rather than silently falling back to a determinable content key.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** = `encryption_enabled`

-   **`oprf_threshold`**
    -   **Description:** The minimum number of distinct daemon OPRF share evaluations required to derive a content key. Must satisfy `1 <= oprf_threshold <= len(daemons)`. Values greater than `1` require the P2P transport to gather peer evaluations.
    -   **Type:** Integer
    -   **Default:** number of configured daemons (all)

### [metrics] (required)

This section controls the behavior of the decentralized polymorphic system and the Prometheus metrics exporter. **This section is mandatory** — the application will return an error if it is missing. The polymorphic system is only active if `polymorphic_system = true` in the `[global]` section. The Prometheus exporter is independent of the polymorphic system and can be used on its own.

-   **`interval`**
    -   **Description:** The interval in seconds at which each daemon samples its local CPU and memory metrics.
    -   **Type:** Integer
    -   **Default:** None (required)

-   **`min_threshold`**
    -   **Description:** The minimum free resource percentage, represented as a float. If free CPU or memory drops below this threshold, it triggers a move to a less robust replication strategy.
    -   **Type:** Float (e.g., `0.1` for 10%)
    -   **Default:** None (required)

-   **`max_threshold`**
    -   **Description:** The maximum used resource percentage, represented as a float. If used CPU or memory rises above this threshold, it also triggers a move to a less robust strategy.
    -   **Type:** Float (e.g., `0.9` for 90%)
    -   **Default:** None (required)

-   **`fallback_interval`**
    -   **Description:** The duration in seconds that the system must remain in a low-load state before it will attempt to switch back to a more robust replication strategy.
    -   **Type:** Integer
    -   **Default:** None (required)

-   **`prometheus_port`**
    -   **Description:** The port on which the Prometheus metrics exporter listens. When set to a positive value, the server starts an HTTP server on the specified port exposing `/metrics` (Prometheus-format text) and `/health` (returns `200 OK`) endpoints. The metrics server runs in a separate goroutine and does not share the accept loop or connection pool with the main daemon. All counters use `sync/atomic` — no locks, no external dependencies.
    -   **Type:** Integer
    -   **Default:** `0` (disabled)
    -   **Example:** `9100`

    **Exported metrics:**

    | Metric | Type | Description |
    |---|---|---|
    | `momo_connections_total` | counter | Total connections accepted |
    | `momo_active_connections` | gauge | Current active connections |
    | `momo_uploads_total` | counter | Total file uploads |
    | `momo_downloads_total` | counter | Total file downloads |
    | `momo_deletes_total` | counter | Total file deletes |
    | `momo_replication_total` | counter | Total replication operations |
    | `momo_errors_total` | counter | Total errors |
    | `momo_bytes_uploaded_total` | counter | Total bytes uploaded (excludes dedup hits) |
    | `momo_bytes_downloaded_total` | counter | Total bytes downloaded |
    | `momo_uptime_seconds` | gauge | Server uptime in seconds |
    | `momo_goroutines` | gauge | Current goroutine count |
    | `momo_memory_alloc_bytes` | gauge | Allocated memory in bytes |
    | `momo_memory_sys_bytes` | gauge | System memory in bytes |
    | `momo_gc_runs_total` | counter | Total GC runs |
    | `momo_build_info` | gauge | Build info (hostname label) |

    **Prometheus scrape config:**
    ```yaml
    scrape_configs:
      - job_name: 'momo'
        static_configs:
          - targets: ['node1:9100', 'node2:9100', 'node3:9100']
    ```

    **Health check:**
    ```bash
    curl http://localhost:9100/health
    # Returns: OK
    ```

### [daemon.N]

The configuration must contain a section for each daemon in the cluster, numbered sequentially starting from `0` (e.g., `[daemon.0]`, `[daemon.1]`). 

**Note:** In the **Balanced Primary** model, any node can act as the primary for a specific object based on its hash. The sequential IDs are used by the CRUSH algorithm to calculate placement.

-   **`host`**
    -   **Description:** The IP address and port for this specific daemon's main service.
    -   **Type:** String
    -   **Example:** `localhost:8080`

-   **`data`**
    -   **Description:** The path to the data storage directory for this daemon.
    -   **Type:** String
    -   **Example:** `/data/0`

-   **`drive`**
    -   **Description:** The device identifier for the drive where the data directory resides. This is used for accurate disk usage monitoring.
    -   **Type:** String
    -   **Example:** `/dev/sda1`

-   **`change_replication`**
    -   **Description:** The IP address and port where this daemon listens for commands to change its replication mode. This is used by the primary server's polymorphic engine to coordinate strategy changes across the cluster.
    -   **Type:** String (host:port)
    -   **Example:** `localhost:9090`

-   **`oprf_share`**
    -   **Description:** The hex-encoded 256-bit Shamir share of the threshold OPRF secret assigned to this daemon (required when `oprf_enabled = true`). Each daemon holds a **distinct** share; no daemon holds the full secret, so no single server can evaluate the OPRF on a dedup tag alone. Shares are produced by a one-time dealer (see `crypto.GenerateOPRFShares`) and distributed out-of-band.
    -   **Type:** String (64 hex characters)
    -   **Example:** `c0ffee...` (64 hex chars)

-   **`oprf_share_index`**
    -   **Description:** The Shamir evaluation point of `oprf_share` (a daemon's index within the secret split). Must be unique across the cluster and within `[1, len(daemons)]`. Defaults to the daemon's 1-based position in the config when unset.
    -   **Type:** Integer
    -   **Default:** daemon position (1-based)

### [p2p]

This section controls the P2P gossip transport layer for decentralized cluster coordination. When enabled, nodes discover each other via gossip, exchange heartbeats with SWIM-style failure detection, and coordinate scatter-gather queries and lease-based consensus for deletes.

-   **`enabled`**
    -   **Description:** Enables or disables the P2P transport layer.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** `false`

-   **`gossip_port`**
    -   **Description:** The port used for P2P gossip communication. This is separate from the main daemon port.
    -   **Type:** String
    -   **Default:** `4450`

-   **`gossip_interval`**
    -   **Description:** The interval in seconds between gossip rounds. Each round sends heartbeats to a random subset of peers.
    -   **Type:** Integer
    -   **Default:** `1`

-   **`suspicion_timeout`**
    -   **Description:** The timeout in seconds before a peer is marked as Suspect (unreachable). A peer is marked Offline after twice this timeout.
    -   **Type:** Integer
    -   **Default:** `5`

-   **`fanout`**
    -   **Description:** The number of peers to contact per gossip round for heartbeat exchange. When **0** (default), fanout is **adaptive** and scales with the number of alive peers as `ceil(ln N)`, bounded to `[1, 10]` — low on small clusters, higher on large clusters for faster convergence (issue #825). A positive value is an explicit fixed override.
    -   **Type:** Integer (`>= 0`; `0` = adaptive)
    -   **Default:** `0` (adaptive)

-   **`ping_timeout`**
    -   **Description:** The timeout in milliseconds for SWIM direct ping/ack probes.
    -   **Type:** Integer
    -   **Default:** `500`

-   **`indirect_ping_count`**
    -   **Description:** The number of peers to ask for indirect pings when a direct ping fails. Capped at 10.
Becomes a SWIM indirect ping fan-out.
    -   **Type:** Integer
    -   **Default:** `3`
    -   **Max:** `10`

-   **`scatter_gather_timeout`**
    -   **Description:** The timeout in seconds for scatter-gather query responses. Queries that don't respond within this window are skipped.
    -   **Type:** Integer
    -   **Default:** `5`

-   **`lease_timeout`**
    -   **Description:** The timeout in seconds for lease-based consensus on destructive operations (delete). Leases are acquired before deletes and released after propagation.
    -   **Type:** Integer
    -   **Default:** `10`

-   **`tls_cert_file`**
    -   **Description:** Path to a PEM-encoded TLS certificate for the node's P2P transport. When set together with `tls_key_file`, P2P traffic is encrypted with TLS. Must be set identically (or with certificates signed by a shared `tls_ca_file`) across all nodes for mutual authentication.
    -   **Type:** String (file path)
    -   **Default:** (none — P2P runs in plaintext with a CRITICAL warning)

-   **`tls_key_file`**
    -   **Description:** Path to the PEM-encoded private key that pairs with `tls_cert_file` for the node's P2P transport.
    -   **Type:** String (file path)
    -   **Default:** (none — P2P runs in plaintext with a CRITICAL warning)

-   **`tls_ca_file`**
    -   **Description:** Path to a PEM-encoded CA certificate used to verify peer certificates. Enables **mutual TLS (mTLS)**: when set, both the local node's certificate and each peer's certificate are validated against this CA (`RequireAndVerifyClientCert`), and peer verification is enforced. If omitted, TLS is still enabled (node presents its own certificate) but peer certificates are not verified against a CA.
    -   **Type:** String (file path)
    -   **Default:** (none — TLS without peer CA verification)

**TLS encryption note:** When `tls_cert_file` and `tls_key_file` are set, the P2P transport negotiates TLS with a minimum version of TLS 1.2. Peer ID authentication is always enforced via `AuthFunc` (a connecting peer's ID must fall within the configured daemon set), independent of TLS. If TLS files are not configured, the node logs a `CRITICAL` warning and falls back to plaintext — do not use in production.

### [storage]

This section controls the Content-Addressable Storage (CAS) engine, including backend selection, garbage collection, and tombstone retention.

-   **`backend`**
    -   **Description:** Selects the blob storage backend. The bbolt metadata database is always stored locally in `daemon.data`; only blob bytes are routed to the configured backend.
    -   **Type:** String
    -   **Default:** `local`
    -   **Valid values:**
        -   `local` — Local filesystem with tiered directory layout (default, backward-compatible)
        -   `nfs` — Local filesystem on an NFS mount (functionally identical to `local`; set `daemon.data` to the NFS mount path)
        -   `s3` — S3-compatible remote storage (requires `s3_*` config fields)
        -   `raw` — Raw block device (requires `raw_device_path` or `daemon.drive`)

-   **`gc_interval`**
    -   **Description:** The interval in seconds between CAS garbage collection sweeps. The GC removes orphaned blobs (refcount=0) and expired tombstones.
    -   **Type:** Integer
    -   **Default:** `300` (5 minutes)

-   **`tombstone_retention`**
    -   **Description:** The duration in seconds to retain tombstones after deletion. Tombstones prevent resurrection of deleted objects and are cleaned up after this period.
    -   **Type:** Integer
    -   **Default:** `86400` (24 hours)

-   **`s3_endpoint`**
    -   **Description:** S3-compatible API endpoint URL (e.g., `https://s3.amazonaws.com`). Only used when `backend = s3`.
    -   **Type:** String
    -   **Default:** (none)

-   **`s3_region`**
    -   **Description:** S3 region name. Only used when `backend = s3`.
    -   **Type:** String
    -   **Default:** (none)

-   **`s3_bucket`**
    -   **Description:** S3 bucket name for blob storage. Only used when `backend = s3`.
    -   **Type:** String
    -   **Default:** (none)

-   **`s3_access_key`**
    -   **Description:** S3 access key ID. Only used when `backend = s3`.
    -   **Type:** String
    -   **Default:** (none)

-   **`s3_secret_key`**
    -   **Description:** S3 secret access key. Only used when `backend = s3`.
    -   **Type:** String
    -   **Default:** (none)

-   **`s3_path_style`**
    -   **Description:** Use path-style addressing (`endpoint/bucket/key`) instead of virtual-host style (`bucket.endpoint/key`). Required for MinIO and most S3-compatible APIs.
    -   **Type:** Boolean
    -   **Default:** `true`

-   **`s3_insecure`**
    -   **Description:** Allow an `http://` (cleartext) S3 endpoint. Defaults to `false`, in which case a non-`https://` endpoint is rejected at startup with an `EINVAL` config error so credentials and blob content are never sent in cleartext. When `true`, the endpoint may use `http://` and a prominent warning is logged at startup. `https://` endpoints are always accepted regardless of this flag.
    -   **Type:** Boolean
    -   **Default:** `false`

-   **`raw_device_path`**
    -   **Description:** Path to the raw block device for blob storage (e.g., `/dev/sda1`). Overrides `daemon.drive` if set. Only used when `backend = raw`.
    -   **Type:** String
    -   **Default:** (none; falls back to `daemon.drive`)

## Example Configurations

### High-Durability Object Storage

```ini
[global]
debug = true
protocol = momo-quic
replication_factor = 5
replication_order = 3,2,1
polymorphic_system = true

[metrics]
interval = 10
min_threshold = 0.1
max_threshold = 0.9
fallback_interval = 30
prometheus_port = 9100

[daemon.0]
host = 10.0.0.1:8080
change_replication = 10.0.0.1:9090
data = /mnt/data/0
drive = /dev/nvme0n1
# ... additional daemons up to N

[p2p]
enabled = true
gossip_port = 4450
gossip_interval = 1
suspicion_timeout = 5
fanout = 3
ping_timeout = 500
indirect_ping_count = 3
scatter_gather_timeout = 5
lease_timeout = 10
# Production deployments should enable TLS to encrypt P2P traffic (see note below)
# tls_cert_file = /etc/momo/p2p.crt
# tls_key_file = /etc/momo/p2p.key
# tls_ca_file = /etc/momo/p2p-ca.crt

[storage]
gc_interval = 300
tombstone_retention = 86400
```

### NFS Storage Backend

To store blobs on an NFS-mounted filesystem, set `backend = nfs` and point `data` to the NFS mount path. The bbolt metadata database is also stored locally on the NFS mount. Note: NFS provides close-to-open consistency, not coherent consistency — suitable for single-writer-per-object workloads.

```ini
[storage]
backend = nfs
gc_interval = 300
tombstone_retention = 86400

[daemon.0]
data = /mnt/nfs/momo/node0
# ...
```

### S3 Storage Backend

To store blobs in an S3-compatible bucket (AWS S3, MinIO, etc.):

```ini
[storage]
backend = s3
s3_endpoint = https://s3.us-east-1.amazonaws.com
s3_region = us-east-1
s3_bucket = momo-blobs
s3_access_key = AKIAIOSFODNN7EXAMPLE      # notsecret
s3_secret_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY  # notsecret
s3_path_style = false
gc_interval = 300
tombstone_retention = 86400
```

### Raw Device Storage Backend

To store blobs directly on a raw block device (no filesystem overhead):

```ini
[storage]
backend = raw
raw_device_path = /dev/sda1
gc_interval = 300
tombstone_retention = 86400
```

### Encrypted QUIC Deployment

To run the cluster securely over UDP using auto-generated TLS 1.3 certificates, simply change the `protocol` field.

```ini
[global]
protocol = momo-quic
auth_token = YOUR_SECURE_64_BYTE_TOKEN_HERE
polymorphic_system = true
# ... (metrics and daemon blocks remain the same)
```

### S3 Compatibility Layer (TCP or QUIC)

To allow standard AWS SDKs (like `aws-cli` or `boto3`) to upload, list, download, and delete files directly into the Momo replication ring, use the `s3-*` protocols.

```ini
[global]
protocol = s3-tcp # Or use s3-quic for secure deployments
polymorphic_system = true
# ... (metrics and daemon blocks remain the same)
```

Now, standard S3 client tools can list, download, and delete files directly over Momo's S3 REST gateway.

#### Examples (using aws-cli):

- **List Objects (ListObjectsV2):**
  ```bash
  aws s3 ls s3://any-bucket-name/ --endpoint-url http://127.0.0.1:4440
  ```

- **Download Object (GetObject):**
  ```bash
  aws s3 cp s3://any-bucket-name/file.txt ./file.txt --endpoint-url http://127.0.0.1:4440
  ```

- **Delete Object (DeleteObject):**
  ```bash
  aws s3 rm s3://any-bucket-name/file.txt --endpoint-url http://127.0.0.1:4440
  ```
