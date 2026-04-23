## 2024-05-24 - Enforce Authentication Handshake
**Vulnerability:** Missing authentication on sensitive endpoints (`Daemon` and `ChangeReplicationModeServer`). Any client could connect and send replication data or files without authorization.
**Learning:** The system architecture lacked a mandatory authentication handshake during the initial connection phase, leaving it open to unauthorized access and potential DoS attacks.
**Prevention:** Always implement an authentication mechanism (e.g., a shared secret or token) for internal services, even if they are not exposed to the public internet. Ensure the token is passed and verified immediately upon connection establishment before processing any further data.
## 2024-05-24 - Prevent Timing Attacks in Authentication Handshake
**Vulnerability:** Standard string inequality comparison () was used to verify the authentication token in  and , making the system vulnerable to timing side-channel attacks.
**Learning:** The time taken to compare strings using  depends on how many initial characters match. An attacker can exploit this by sending a series of incorrect tokens and measuring the response time to incrementally deduce the correct token, bypassing authentication.
**Prevention:** Always use  when verifying secrets, tokens, or hashes to ensure the comparison time is constant regardless of the input, mitigating timing attacks.
## 2024-05-24 - Prevent Timing Attacks in Authentication Handshake
**Vulnerability:** Standard string inequality comparison (`!=`) was used to verify the authentication token in `server.go` and `replication.go`, making the system vulnerable to timing side-channel attacks.
**Learning:** The time taken to compare strings using `!=` depends on how many initial characters match. An attacker can exploit this by sending a series of incorrect tokens and measuring the response time to incrementally deduce the correct token, bypassing authentication.
**Prevention:** Always use `crypto/subtle.ConstantTimeCompare` when verifying secrets, tokens, or hashes to ensure the comparison time is constant regardless of the input, mitigating timing attacks.
