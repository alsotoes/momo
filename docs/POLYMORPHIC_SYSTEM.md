# Momo's Polymorphic System

The most unique aspect of Momo is its ability to change replication strategies at runtime. This "polymorphic" nature allows the system to be highly adaptive and resilient.

## How It Works

Momo utilizes a decentralized polymorphic engine. The **metrics component** runs on **every node** in the cluster. This component is responsible for the following:

1.  **Monitoring System Metrics:** Each metrics component periodically samples the local CPU and memory usage. The sampling interval is configurable in `momo.conf`.

2.  **Evaluating Thresholds:** The collected metrics are compared against predefined thresholds:
    *   **`min_threshold`**: If both CPU and memory usage are below this threshold, it indicates a low system load.
    *   **`max_threshold`**: If either CPU or memory usage rises above this threshold, it indicates a high system load.

3.  **Triggering Strategy Changes:** When a threshold is breached, the node initiates a change in the cluster's replication strategy.
    *   **Under high load:** Switch to a *less* robust strategy (e.g., Splay -> Chain).
    *   **Under low load:** After a `fallback_interval`, switch to a *more* robust strategy (e.g., Chain -> Splay).

4.  **Cluster-Wide Broadcast:** When a node decides on a new replication strategy, it **broadcasts** this change to **all other daemons** in the cluster via their configured `change_replication` endpoint. This ensures that every potential "Primary" node (as determined by CRUSH) stays synchronized with the current cluster policy.

### Decision Flow Diagram

```
                      +-----------------------------+
                      | Metrics Component (Any Node)|
                      | (Reads local CPU/Memory)    |
                      +-----------------------------+
                                     |
                                     v
+-------------------------------------------------------------------------+
|                   Is CPU or Memory usage > max_threshold?               |
+-------------------------------------------------------------------------+
          |                                                 |
        (Yes)                                             (No)
          |                                                 |
          v                                                 v
+-----------------------------+               +----------------------------------+
|      HIGH SYSTEM LOAD       |               |         NORMAL SYSTEM LOAD       |
+-----------------------------+               +----------------------------------+
          |                                                 |
          v                                                 | Is CPU and Memory usage < min_threshold
+-----------------------------+                             | AND has `fallback_interval` passed
| Switch to LESS robust       |                             | since the last HIGH load event?
| strategy AND broadcast to   |                             |
| all nodes in config.        |                             +---------------------------------+
+-----------------------------+                                       |                |
                                                                      (Yes)            (No)
                                                                      |                |
                                                                      v                v
                                                          +--------------------------+  (Do Nothing)
                                                          | Switch to MORE robust     |
                                                          | strategy AND broadcast to |
                                                          | all nodes in config.      |
                                                          +---------------------------+
```

## Example Scenario

Consider a `replication_order` of `3,2,1,4` which maps to `primary-splay, splay, chain, none`.

1.  The system starts in **primary-splay** mode (mode `3`).
2.  A large number of files are uploaded, causing CPU usage to exceed the `max_threshold`.
3.  The metrics component detects this and switches the strategy to **splay** (mode `2`), which is less demanding.
4.  If the load continues to increase and breaches the threshold again, the system will step right again to **chain** (mode `1`).
5.  Once the file uploads are complete and the system load remains low (below `min_threshold`) for the duration of the `fallback_interval`, the metrics component will switch the strategy back to **splay** (mode `2`) and, if conditions remain calm, eventually back to **primary-splay** (mode `3`).

## Benefits of a Polymorphic System

This adaptive capability provides several key advantages:

*   **Optimal Performance:** By dynamically adjusting to the current workload, the system avoids being overwhelmed during periods of high traffic.
*   **Enhanced Data Redundancy:** During periods of low activity, the system can automatically switch to a more robust replication strategy, maximizing data safety without manual intervention.
*   **Resilience:** The system can gracefully handle sudden spikes in load without failing, simply by degrading its replication strategy temporarily.
*   **Efficiency:** Resources are used more effectively, as the system only employs resource-intensive strategies when it has the capacity to do so.

In essence, the polymorphic system allows Momo to strike a dynamic balance between performance and data redundancy, making it a powerful and intelligent replication solution.
