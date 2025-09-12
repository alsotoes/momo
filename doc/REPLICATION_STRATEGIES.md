# Momo Replication Strategies

This document describes the different data replication strategies that Momo can employ. The active strategy is determined by the `replication_order` in `momo.conf` and can be changed at runtime if the polymorphic system is enabled.

Each strategy offers a different balance between write performance, data redundancy, and network overhead.

---

## No Replication

-   **Mode Code:** `1`

This is the most basic mode. Data is written only to the primary server (Daemon 0) and is not replicated to any other servers in the cluster.

**Data Flow:**
The client sends the file directly to the primary server, which writes it to its local storage. No other network traffic is generated.

```
+----------+           +-----------------+
|  Client  | --------> | Primary Server  |
+----------+           | (writes to disk)| 
                       +-----------------+
```

**Trade-offs:**
-   **Pros:** Fastest possible write speed. Minimal network and CPU overhead.
-   **Cons:** No data redundancy. If the primary server fails, any data written in this mode is lost.

---

## Chain Replication

-   **Mode Code:** `2`

In this mode, the servers are organized into a linear chain. The primary server writes the data and forwards it to the first secondary, which then forwards it to the second, and so on, until the end of the chain is reached.

**Data Flow:**
The client sends the file to the primary, which initiates a sequential replication process down the chain.

```
+----------+     +---------+     +---------+     +---------+
|  Client  | --> | Primary | --> | Server 1| --> | Server 2| --> ...
+----------+     +---------+     +---------+     +---------+
```

**Trade-offs:**
-   **Pros:** Less network load on the primary compared to Splay, as it only sends one copy. Reads can be consistently served from the tail of the chain.
-   **Cons:** Highest write latency, as the total time is the sum of all transfers along the chain. A single server failure breaks the chain and causes the replication to fail.

---

## Splay Replication

-   **Mode Code:** `3`

In Splay mode, the primary server sends the data to all secondary servers simultaneously (in parallel).

**Data Flow:**
The client sends the file to the primary. The primary then becomes a central hub, "splaying" the data out to all other servers at the same time.

```
                       +-----------+
                     / | Server 1  |
                   /   +-----------+
                 /
+----------+     +---------+     +-----------+
|  Client  | --> | Primary | --> | Server 2  |
+----------+     +---------+     +-----------+
                 \
                   \   +-----------+
                     \ | Server 3  |
                       +-----------+
```

**Trade-offs:**
-   **Pros:** Lower write latency than Chain Replication, as all secondary copies are transferred in parallel. More resilient to single secondary failures.
-   **Cons:** Very high network bandwidth and CPU usage on the primary server, as it must manage and send `N-1` copies of the data simultaneously.

---

## Primary-Splay Replication

-   **Mode Code:** `4`

In this mode, the client sends the data to all servers in the cluster simultaneously (in parallel). This distributes the network load and provides the fastest replication to all nodes.

**Data Flow:**
The client establishes a connection with every server in the cluster and sends the file to all of them at the same time.

```
                 +-----------+
               / | Server 0  |
             /   +-----------+
           /
+----------+     +-----------+
|  Client  | --> | Server 1  |
+----------+     +-----------+
           \ 
             \   +-----------+
               \ | Server 2  |
                 +-----------+
```

**Trade-offs:**
-   **Pros:** Provides the highest level of data redundancy with the lowest latency, as all servers receive the data at roughly the same time. Distributes the network load across all servers instead of concentrating it on the primary.
-   **Cons:** Requires the client to have more complex logic to manage parallel uploads and handle failures for each server individually. Increases network traffic originating from the client.
