## 2024-05-24 - Data Race on Global Replication State
**Vulnerability:** A data race existed where the global variables `CurrentReplicationMode` and `ReplicationState` were updated by `ChangeReplicationModeServer` and read by `Daemon` across multiple network connection-handling goroutines without synchronization.
**Learning:** In Go, concurrent network connection handlers that modify or read shared global application state (like replication configurations) must use explicit synchronization (e.g., `sync.RWMutex`), as implicit thread-safety does not exist.
**Prevention:** Always encapsulate global state modification and access within mutex-protected getter and setter functions. Unexport the global variables to enforce compiler checks.
