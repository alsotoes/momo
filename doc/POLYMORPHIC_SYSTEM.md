# Momo's Polymorphic System

The most unique aspect of Momo is its ability to change replication strategies at runtime. This "polymorphic" nature allows the system to be highly adaptive and resilient.

## How It Works

The core of the polymorphic system is the **metrics component**, which runs exclusively on **Server 0**. This component is responsible for the following:

1.  **Monitoring System Metrics:** The metrics component periodically samples the CPU and memory usage of the system. The sampling interval is configurable in `momo.conf`.

2.  **Evaluating Thresholds:** The collected metrics are compared against predefined thresholds, also configurable in `momo.conf`:
    *   **`min_threshold`**: If the free CPU or memory drops below this threshold, it indicates high system load.
    *   **`max_threshold`**: If the used CPU or memory rises above this threshold, it also indicates high system load.

3.  **Triggering Strategy Changes:** When a threshold is breached, the system initiates a change in the replication strategy. The order of strategies is defined by `replication_order` in the configuration.
    *   **Under high load:** The system will switch to a *less* resource-intensive replication strategy (e.g., from Splay to Chain, or Chain to No Replication). This is considered a "step left" in the `replication_order` list.
    *   **Under low load:** After a configurable `fallback_interval` without high-load events, the system will switch to a *more* robust, and potentially more resource-intensive, strategy. This is a "step right" in the `replication_order` list.

4.  **Propagating Changes:** When Server 0 decides on a new replication strategy, it communicates this change to all other servers in the cluster to ensure a consistent state.

### Decision Flow Diagram

The following diagram illustrates the decision-making process of the metrics component:

```
                      +-----------------------------+
                      |   Metrics Component on S0   |
                      | (Reads CPU/Memory stats)    |
                      +-----------------------------+
                                     |
                                     v
+-------------------------------------------------------------------------+
|                Is CPU/Memory usage > max_threshold OR                   |
|                   free CPU/Memory < min_threshold?                      |
+-------------------------------------------------------------------------+
          |                                                 |
        (Yes)                                             (No)
          |                                                 |
          v                                                 v
+-----------------------------+               +----------------------------------+
|      HIGH SYSTEM LOAD       |               |         NORMAL SYSTEM LOAD       |
+-----------------------------+               +----------------------------------+
          |                                                 |
          v                                                 |
+-----------------------------+                             | Has `fallback_interval` passed
| Switch to LESS robust       |                             | since the last HIGH load event?
| replication strategy        |                             |
| (Step Left in order list)   |                             +---------------------------------+
| (e.g. Splay -> Chain)       |                                       |                |
+-----------------------------+                                     (Yes)            (No)
                                                                      |                |
                                                                      v                v
                                                          +--------------------------+  (Do Nothing)
                                                          | Switch to MORE robust     |
                                                          | replication strategy      |
                                                          | (Step Right in order list)|
                                                          | (e.g. Chain -> Splay)     |
                                                          +---------------------------+
```

## Example Scenario

Consider a `replication_order` of `2,1,0` (Splay, Chain, No Replication).

1.  The system starts in **Splay Replication** mode (mode `2`).
2.  A large number of files are uploaded, causing CPU usage to exceed the `max_threshold`.
3.  The metrics component detects this and switches the strategy to **Chain Replication** (mode `1`), which is less demanding on the primary server.
4.  If the load continues to increase and breaches the threshold again, the system might further step down to **No Replication** (mode `0`).
5.  Once the file uploads are complete and the system load remains low for the duration of the `fallback_interval`, the metrics component will switch the strategy back to **Chain Replication** (mode `1`) and eventually back to **Splay Replication** (mode `2`).

## Benefits of a Polymorphic System

This adaptive capability provides several key advantages:

*   **Optimal Performance:** By dynamically adjusting to the current workload, the system avoids being overwhelmed during periods of high traffic.
*   **Enhanced Data Redundancy:** During periods of low activity, the system can automatically switch to a more robust replication strategy, maximizing data safety without manual intervention.
*   **Resilience:** The system can gracefully handle sudden spikes in load without failing, simply by degrading its replication strategy temporarily.
*   **Efficiency:** Resources are used more effectively, as the system only employs resource-intensive strategies when it has the capacity to do so.

In essence, the polymorphic system allows Momo to strike a dynamic balance between performance and data redundancy, making it a powerful and intelligent replication solution.
