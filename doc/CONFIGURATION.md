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

-   **`replication_order`**
    -   **Description:** A comma-separated list of integers that defines the sequence of replication strategies the polymorphic system can cycle through. The order determines the path of escalation and de-escalation based on system load.
    -   **Type:** Comma-separated list of integers (e.g., `1,2,3,4`)
    -   **Possible Values:** Each integer corresponds to a replication strategy:
        -   `1`: primary-splay
        -   `2`: splay
        -   `3`: chain
        -   `4`: none
    -   **Default:** `1,2,3,4`

-   **`polymorphic_system`**
    -   **Description:** When set to `true`, enables the polymorphic engine on the primary server (daemon 0), allowing the cluster to change replication strategies dynamically based on system load.
    -   **Type:** Boolean (`true` or `false`)
    -   **Default:** `true`

### [metrics]

This section controls the behavior of the polymorphic replication system. It is only active if `polymorphic_system = true` in the `[global]` section.

-   **`interval`**
    -   **Description:** The interval in seconds at which the primary server samples CPU and memory metrics.
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

The configuration must contain a section for each daemon in the cluster, numbered sequentially starting from `0` (e.g., `[daemon.0]`, `[daemon.1]`). **Daemon 0 is always the primary server.**

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

## Example Configuration

```ini
# Momo Server Configuration Example
[global]
debug = true
replication_order = 1,2,3,4
polymorphic_system = true

[metrics]
interval = 10
min_threshold = 0.1
max_threshold = 0.9
fallback_interval = 30

# Daemon 0 - The Primary Server
[daemon.0]
host = localhost:8080
change_replication = localhost:9090
data = /data/0
drive = /dev/sda1

# Daemon 1 - A Secondary Server
[daemon.1]
host = localhost:8081
change_replication = localhost:9091
data = /data/1
drive = /dev/sdb1

# Daemon 2 - Another Secondary Server
[daemon.2]
host = localhost:8082
change_replication = localhost:9092
data = /data/2
drive = /dev/sdc1
```
