## 2026-03-09 - [Denial of Service via Unbounded JSON Decode]
**Vulnerability:** The server accepts configuration changes via an open port without size limits and reads JSON directly from the connection (`json.NewDecoder(connection)`).
**Learning:** A malicious actor can hold connections open or stream infinite spaces/large payloads, leading to memory exhaustion or goroutine leaks.
**Prevention:** Always use `io.LimitReader` when decoding JSON from untrusted network connections to enforce a maximum payload size, and use connection read/write deadlines (`SetDeadline`) to prevent slowloris attacks.
