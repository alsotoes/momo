## 2026-03-10 - Path Traversal during file replication metadata retrieval
**Vulnerability:** The application was vulnerable to path traversal during peer replication because it retrieved the `fileName` directly from client-supplied metadata and passed it to downstream handlers (`getFile` and `connectToPeer`) without early sanitization.
**Learning:** Even if `filepath.Base` is used right before `os.Create` in `getFile()`, it was missed in `server.go` when `connectToPeer` built replication paths (`daemons[x].Data + "/" + metadata.Name`). It's better to sanitize the metadata *as soon as it's parsed* from the network.
**Prevention:** Always validate and sanitize user-provided identifiers (like file paths) immediately at the input boundary before they are returned to other components of the system.
## 2026-03-11 - Cryptographically weak MD5 hash used for integrity checks
**Vulnerability:** Use of MD5 hashing for file integrity verification during network transfers. MD5 is susceptible to collision attacks, allowing a malicious actor to craft a different file with the same hash.
**Learning:** MD5 was used as a legacy choice for simple integrity checks, but it fails to provide modern security guarantees against intentional tampering.
**Prevention:** Always use cryptographically secure hashing algorithms like SHA-256 or SHA-3 for integrity checks and digital signatures, even if the primary goal is just "integrity logging".
## 2026-03-11 - Missing timeout on main network listener connection
**Vulnerability:** The primary server file upload Daemon lacked a connection deadline (`connection.SetDeadline`). This allowed attackers to perform Slowloris attacks or simply keep TCP connections open indefinitely, potentially exhausting server socket limits and causing a DoS.
**Learning:** While absolute deadlines (`connection.SetDeadline`) prevent slowloris, they break functionality for endpoints handling large, slow data streams like file uploads because the deadline expires regardless of active transfer progress. An idle timeout (rolling deadline) is required.
**Prevention:** Use an `IdleTimeoutConn` wrapper that resets `SetReadDeadline` and `SetWriteDeadline` upon every successful read/write operation for streams that take variable time to process.
