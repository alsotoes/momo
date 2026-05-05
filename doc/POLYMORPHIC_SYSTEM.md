# Momo's Polymorphic System

The most unique aspect of Momo is its ability to change replication strategies at runtime. This "polymorphic" nature allows the system to be highly adaptive and resilient.

## How It Works

The core of the polymorphic system is the **metrics component**, which runs exclusively on the primary server (**Daemon 0**). This component is responsible for the following:

1.  **Monitoring System Metrics:** The metrics component periodically samples the CPU and memory usage of the system. The sampling interval is configurable in `momo.conf`.

2.  **Evaluating Thresholds:** The collected metrics are compared against predefined thresholds, also configurable in `momo.conf`:
    *   **`min_threshold`**: If both CPU and memory usage are below this threshold, it indicates a low system load.
    *   **`max_threshold`**: If either CPU or memory usage rises above this threshold, it indicates a high system load.

3.  **Triggering Strategy Changes:** When a threshold is breached, the system initiates a change in the replication strategy. The order of strategies is defined by the `replication_order` list in the configuration.
    *   **Under high load:** The system will switch to a *less* robust, and less resource-intensive, replication strategy. This is a **"step right"** in the `replication_order` list (moving to a higher index).
    *   **Under low load:** After a configurable `fallback_interval` without high-load events, the system will switch to a *more* robust replication strategy. This is a **"step left"** in the `replication_order` list (moving to a lower index).

4.  **Propagating Changes:** When Daemon 0 decides on a new replication strategy, it communicates this change to all other daemons in the cluster via their configured `change_replication` endpoint to ensure a consistent state.

### Decision Flow Diagram

The following diagram illustrates the decision-making process of the metrics component:

```
                      +-----------------------------+
                      |   Metrics Component on D0   |
                      | (Reads CPU/Memory stats)    |
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
| replication strategy        |                             |
| (Step Right in order list)  |                             +---------------------------------+
| (e.g. splay -> chain)       |                                       |                |
+-----------------------------+                                     (Yes)            (No)
                                                                      |                |
                                                                      v                v
                                                          +--------------------------+  (Do Nothing)
                                                          | Switch to MORE robust     |
                                                          | replication strategy      |
                                                          | (Step Left in order list) |
                                                          | (e.g. chain -> splay)     |
                                                          +---------------------------+
```

## Example Scenario

Consider a `replication_order` of `1,2,3,4` which maps to `primary-splay, splay, chain, none`.

1.  The system starts in **primary-splay** mode (mode `1`).
2.  A large number of files are uploaded, causing CPU usage to exceed the `max_threshold`.
3.  The metrics component detects this and switches the strategy to **splay** (mode `2`), which is less demanding.
4.  If the load continues to increase and breaches the threshold again, the system will step right again to **chain** (mode `3`).
5.  Once the file uploads are complete and the system load remains low (below `min_threshold`) for the duration of the `fallback_interval`, the metrics component will switch the strategy back to **splay** (mode `2`) and, if conditions remain calm, eventually back to **primary-splay** (mode `1`).

## Benefits of a Polymorphic System

This adaptive capability provides several key advantages:

*   **Optimal Performance:** By dynamically adjusting to the current workload, the system avoids being overwhelmed during periods of high traffic.
*   **Enhanced Data Redundancy:** During periods of low activity, the system can automatically switch to a more robust replication strategy, maximizing data safety without manual intervention.
*   **Resilience:** The system can gracefully handle sudden spikes in load without failing, simply by degrading its replication strategy temporarily.
*   **Efficiency:** Resources are used more effectively, as the system only employs resource-intensive strategies when it has the capacity to do so.

In essence, the polymorphic system allows Momo to strike a dynamic balance between performance and data redundancy, making it a powerful and intelligent replication solution.
