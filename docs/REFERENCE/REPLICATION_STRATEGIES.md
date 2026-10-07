# Momo Replication Strategies

This document describes the different data replication strategies that Momo can employ. The active strategy is determined by the `replication_order` in `momo.conf` and can be changed at runtime if the polymorphic system is enabled.

Each strategy offers a different balance between write performance, data redundancy, and network overhead.

In the **Balanced Primary** model, the primary node for any given object is deterministically chosen using the CRUSH-lite algorithm based on the content hash. The total number of copies (primary + secondaries) is determined by the global **`replication_factor`**.

---

## Chain Replication

-   **Mode Code:** `1`

The servers are organized into a linear chain based on the CRUSH placement list. The primary server writes the data and forwards it to the first secondary, which then forwards it to the second, and so on, until the **`replication_factor`** is reached.

**Data Flow:**
The client sends the file to the primary, which initiates a sequential replication process down the chain.

```
+----------+     +---------+     +---------+     +---------+
|  Client  | --> | Primary | --> | Second 1| --> | Second 2| --> ... (up to factor)
+----------+     +---------+     +---------+     +---------+
```

**Trade-offs:**
-   **Pros:** Less network load on the primary compared to Splay, as it only sends one copy. Reads can be consistently served from the tail of the chain.
-   **Cons:** Highest write latency, as the total time is the sum of all transfers along the chain. A single server failure breaks the chain and causes the replication to fail.

---

## Splay Replication

-   **Mode Code:** `2`

The primary server sends the data to `n-1` secondary servers in the CRUSH list simultaneously (where `n = replication_factor`).

**Data Flow:**
The client sends the file to the primary. The primary then becomes a central hub, "splaying" the data out to the other servers in its placement list.

```
                                  +-----------+
                                / | Second 1  |
                              /   +-----------+
                            /
+----------+     +----------+     +-----------+
|  Client  | --> | Primary  | --> | Second 2  |
+----------+     +----------+     +-----------+
                            \
                              \   +-----------+
                                \ | ....      |
                                  +-----------+
```

**Trade-offs:**
-   **Pros:** Lower write latency than Chain Replication, as all secondary copies are transferred in parallel. More resilient to single secondary failures.
-   **Cons:** Very high network bandwidth and CPU usage on the primary server, as it must manage and send `N-1` copies of the data simultaneously.

---

## Primary-Splay Replication

-   **Mode Code:** `3`

In this mode, the client sends the data to all `n` servers in the CRUSH placement list simultaneously (where `n = replication_factor`). This distributes the network load and provides the fastest replication.

**Data Flow:**
The client establishes a connection with every server in the placement list and sends the file to all of them at the same time.

```
                 +-----------+
               / | Primary   |
             /   +-----------+
           /
+----------+     +-----------+
|  Client  | --> | Second 1  |
+----------+     +-----------+
           \ 
             \   +-----------+
               \ | Second 2  |
                 +-----------+
```

**Trade-offs:**
-   **Pros:** Provides the highest level of data redundancy with the lowest latency, as all servers receive the data at roughly the same time. Distributes the network load across all servers instead of concentrating it on the primary.
-   **Cons:** Requires the client to have more complex logic to manage parallel uploads and handle failures for each server individually. Increases network traffic originating from the client.

---

## No Replication (Internal Termination Signal)

-   **Mode Code:** `0`

This mode is used **internally** by the cluster to signal that an object has reached its final destination in a replication sequence (e.g., at the end of a chain or as a parallel write target). It explicitly overrides the global factor to `1`. 

**Note:** This mode should not be used in the `replication_order` configuration.

**Data Flow:**
The server receiving this mode writes the file directly to its local storage and returns an ACK without further forwarding.

```
+----------+           +-----------------+
|  Sender  | --------> | Target Server   |
+----------+           | (writes to disk)| 
                       +-----------------+
```

**Trade-offs:**
-   **Pros:** Fastest possible write speed. Minimal network and CPU overhead.
-   **Cons:** No data redundancy. If the primary server fails, any data written in this mode is lost.
