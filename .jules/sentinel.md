## 2024-05-24 - Enforce Authentication Handshake
**Vulnerability:** Missing authentication on sensitive endpoints (`Daemon` and `ChangeReplicationModeServer`). Any client could connect and send replication data or files without authorization.
**Learning:** The system architecture lacked a mandatory authentication handshake during the initial connection phase, leaving it open to unauthorized access and potential DoS attacks.
**Prevention:** Always implement an authentication mechanism (e.g., a shared secret or token) for internal services, even if they are not exposed to the public internet. Ensure the token is passed and verified immediately upon connection establishment before processing any further data.
