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

-   **`debug`**
    -   **Description:** When set to `true`, enables verbose debug logging for all daemons in the cluster.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** `false`

- **`replication_order`**
    -   **Description:** A comma-separated list of integers that defines the sequence of replication strategies the polymorphic system can cycle through. The order determines the path of escalation and de-escalation based on system load.
    -   **Type:** Comma-separated list of integers (e.g., `3,2,1`)
    -   **Possible Values:** Each integer corresponds to a replication strategy:
        -   `1`: Chain Replication (0 -> 1 -> 2)
        -   `2`: Splay Replication (0 -> 1, 0 -> 2)
        -   `3`: Primary-Splay Replication (Client -> 0, 1, 2)
    -   **Default:** `3,2,1`
    -   **Note:** Mode `0` (No Replication) is used internally by the cluster to signal the end of a replication sequence and should not be included in the configuration.


-   **`replication_factor`**
    -   **Description:** Defines the target number of physical copies (replicas) to maintain for every object in the cluster. Momo uses the CRUSH-lite algorithm to select this many distinct nodes for storage.
    -   **Type:** Integer
    -   **Default:** `3`
    -   **Logic:** If the cluster contains fewer than `replication_factor` nodes, the system will store as many copies as possible and log a warning (**Degraded Mode**).

-   **`polymorphic_system`**
    -   **Description:** When set to `true`, enables the polymorphic engine on the primary server (daemon 0), allowing the cluster to change replication strategies dynamically based on system load.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** `true`

-   **`protocol`**
    -   **Description:** Defines the transport layer used for all intra-cluster and client-server communication.
    -   **Type:** String
    -   **Possible Values:**
        -   `momo-tcp`: High-performance raw TCP transport.
        -   `momo-quic`: Modern encrypted transport running over UDP utilizing TLS 1.3 and QUIC streams.
        -   `s3-tcp`: AWS S3-compatible REST API mapping over standard TCP.
        -   `s3-quic`: AWS S3-compatible REST API mapping over secure QUIC streams.
    -   **Default:** `momo-tcp` (if omitted, falls back to `momo-tcp` with a warning log)

### [metrics]

This section controls the behavior of the decentralized polymorphic system. It is only active if `polymorphic_system = true` in the `[global]` section.

-   **`interval`**
    -   **Description:** The interval in seconds at which each daemon samples its local CPU and memory metrics.
    -   **Type:** Integer
    -   **Default:** `10`

-   **`min_threshold`**
    -   **Description:** The minimum free resource percentage, represented as a float. If free CPU or memory drops below this threshold, it triggers a move to a less robust replication strategy.
    -   **Type:** Float (e.g., `0.1` for 10%)
    -   **Default:** `0.1`

-   **`max_threshold`**
    -   **Description:** The maximum used resource percentage, represented as a float. If used CPU or memory rises above this threshold, it also triggers a move to a less robust strategy.
    -   **Type:** Float (e.g., `0.9` for 90%)
    -   **Default:** `0.9`

-   **`fallback_interval`**
    -   **Description:** The duration in seconds that the system must remain in a low-load state before it will attempt to switch back to a more robust replication strategy.
    -   **Type:** Integer
    -   **Default:** `30`

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

[daemon.0]
host = 10.0.0.1:8080
change_replication = 10.0.0.1:9090
data = /mnt/data/0
drive = /dev/nvme0n1
# ... additional daemons up to N
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
