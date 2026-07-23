## 2025-03-13 - Path Traversal bypass with '..'
**Vulnerability:** Path traversal bypassing `filepath.Base()` using a filename of `..` or `.`.
**Learning:** While `filepath.Base()` extracts the last element of a path (e.g. `../../etc/passwd` becomes `passwd`), it specifically returns `..` and `.` when the input is purely those characters. When this is joined with a storage path (e.g. `filepath.Join("/data", "..")`), the resulting path resolves to the parent directory (`/`), escaping the intended sandbox.
**Prevention:** In addition to using `filepath.Base()`, explicitly validate that the resulting filename is not `.`, `..`, `/`, or `\`.

## 2025-03-14 - File Integrity Check Bypass
**Vulnerability:** The application calculates a SHA-256 hash for received files but fails to assert equality with the `expectedHash` before acknowledging the transfer and saving the file.
**Learning:** Computing a security checksum or hash does not provide security unless the value is actively validated against an expected standard and action is taken (like rejecting the file) upon failure. Logging the hash is insufficient for security.
**Prevention:** Always follow checksum computation with an explicit comparison against the expected value and handle mismatches by aborting the operation and cleaning up partial/invalid artifacts.

## 2025-03-19 - Disk Exhaustion DoS via Unbounded Resource Allocation
**Vulnerability:** The server parsed and accepted file sizes up to the maximum capacity of an `int64` without any validation against upper bounds or negative values. This could allow an attacker to send a maliciously large file size, causing the server to exhaust disk space or other resources when attempting to process the file transfer (Disk Exhaustion DoS).
**Learning:** Network endpoints that allocate resources (like file storage) based on client-provided metadata must validate that metadata against strict upper bounds to prevent resource exhaustion attacks.
**Prevention:** Always define and enforce a `MaxFileSize` limit (or similar resource bound) before accepting and processing data streams from untrusted clients. Also check for negative sizes which might cause integer overflow/underflow issues in downstream logic.

## 2025-03-21 - Data Destruction via Insecure File Upload Handling
**Vulnerability:** The server used `os.Create(fullPath)` to write incoming files, immediately truncating any existing file with the same name. If an attacker uploaded a file with the same name as an existing important file and deliberately supplied a bad hash (or dropped the connection), the `defer` block would delete the file or leave it truncated. This is a critical data destruction/DoS vulnerability.
**Learning:** Writing directly to the final destination path before completing all validations (including hash verification and completion checks) exposes existing data to tampering, truncation, or deletion by unauthenticated/unverified inputs.
**Prevention:** Always write uploaded or network-transferred data to a temporary file (`.tmp`). Only after the entire transfer is complete and all security checks (e.g., hash validation) pass, safely commit the file by closing it and using an atomic `os.Rename(tempPath, finalPath)`.

## 2025-03-25 - Denial of Service via Hanging Outbound Connections
**Vulnerability:** The application used `net.DialTCP` for outbound network connections without any timeout configured. An attacker or a malfunctioning network node could keep the connection open indefinitely, exhausting local file descriptors and causing a Denial of Service (DoS).
**Learning:** Network endpoints must never establish outbound connections without a defined maximum duration. Standard `Dial` or `DialTCP` calls can hang forever if the destination IP is blackholed or drops packets without responding.
**Prevention:** Always use `net.DialTimeout` (or `http.Client` with explicit `Timeout` values) when establishing outbound connections to ensure the application fails fast and releases resources when a remote endpoint is unresponsive.

## 2024-03-24 - DoS via Server Crash on Accept Errors
**Vulnerability:** The server called `os.Exit(1)` inside the main `server.Accept()` loops upon encountering any network error. This allows a trivial Denial of Service (DoS) attack, as exhausting temporary resources (like `EMFILE` for open file descriptors) would crash the entire application instead of allowing it to recover gracefully.
**Learning:** Network `Accept()` loops operate in long-running daemon processes where transient errors are expected under load or adversarial conditions. Crashing the process entirely on a transient error turns a temporary bottleneck into a permanent outage.
**Prevention:** In long-running network daemon loops, log the `Accept()` error, implement a brief sleep (`time.Sleep`) to prevent high CPU spinning, and use `continue` to keep the server alive and processing subsequent requests once resources free up. Never use `os.Exit(1)` or `panic` on expected runtime network errors.

## 2026-03-25 - Unauthenticated file uploads and replication changes
**Vulnerability:** The daemon accepted connections and proceeded to read the file timestamp, metadata and content directly without validating the caller. An authentication handshake was missing.
**Learning:** Accepting network connections and processing data without a verification step (authentication) allows unauthorized users to interact with the system, leading to various security risks including unauthorized data upload and system configuration changes.
**Prevention:** Always implement a mandatory authentication handshake at the beginning of any network communication before processing any protocol-specific data.

## 2024-05-24 - Timing Attack on AuthToken Validation
**Vulnerability:** The server used standard Go string comparison `string(bufferAuthToken) != cfg.Global.AuthToken` to validate the client's authentication token. This exposed the system to timing attacks, where an attacker could deduce the correct token length and contents by measuring response times.
**Learning:** Even simple string equality checks in security contexts can be dangerous in Go. Additionally, when using `subtle.ConstantTimeCompare`, both byte slices must be exactly the same length or the function will immediately return 0, which still leaks length information.
**Prevention:** Always use `crypto/subtle.ConstantTimeCompare` for verifying cryptographic secrets and tokens. Ensure the expected token is properly padded to match the protocol's fixed length before comparison.

## 2024-05-25 - Denial of Service via os.Exit in Accept Loop
**Vulnerability:** The server called `os.Exit(1)` when `server.Accept()` returned an error (such as transient EMFILE/ENFILE errors). An attacker could intentionally or unintentionally trigger a flood of connections, exhausting file descriptors and causing the server to crash.
**Learning:** Crashing the entire application on transient network accept errors creates a critical Denial of Service vulnerability. `Accept()` errors are often temporary resource constraints, not fatal application state errors.
**Prevention:** In network listener loops, log the error and use a brief `time.Sleep(10 * time.Millisecond)` to prevent high CPU spinning on persistent errors like `EMFILE`, and `continue` the loop rather than crashing the process.

## 2026-04-27 - Missing Network Authentication Boundary
**Vulnerability:** The `Daemon` and `ChangeReplicationModeServer` network endpoints accepted and processed protocol data before any authentication occurred, allowing unauthenticated connections to send data and potentially exploit other vulnerabilities.
**Learning:** Security controls like authentication must be enforced at the outermost boundary of the application, before any potentially vulnerable parsing or processing logic is executed.
**Prevention:** Require a mandatory authentication handshake (e.g., sending a padded `AuthToken`) as the very first operation upon establishing a connection, and terminate the connection immediately if the handshake fails.

## 2025-03-27 - Missing Authentication on Network Endpoints
**Vulnerability:** The application's network endpoints (`Daemon` and `ChangeReplicationModeServer`) accepted connections and processed data/state-changes from any client without authentication. This allowed unauthorized clients to upload arbitrary files or alter the cluster replication state.
**Learning:** Network endpoints handling state changes or file storage must authenticate clients immediately upon connection to prevent unauthorized access and system abuse. Plain TCP connections over untrusted networks cannot rely on obscurity.
**Prevention:** Enforce a mandatory authentication handshake (e.g., verifying a fixed-length null-padded `AuthToken` using `crypto/subtle.ConstantTimeCompare`) before parsing any protocol data. Reject unauthorized connections immediately.
## 2026-04-29 - Denial of Service via Outbound Connection Leak
**Vulnerability:** The application used `net.DialTimeout` for outbound network connections but failed to apply any idle timeout to the returned `net.Conn`. If a remote peer accepted the connection but never sent data or closed it, the local goroutine reading/writing to it would block indefinitely, leading to a resource leak (goroutines and file descriptors) and eventual Denial of Service (DoS).
**Learning:** Establishing a network connection with a timeout only protects the initial TCP handshake. It does not protect against a slowloris-style attack or an unresponsive peer holding the connection open without transmitting data.
**Prevention:** Always wrap outbound `net.Conn` instances with an idle timeout mechanism (e.g., using `SetReadDeadline` and `SetWriteDeadline` on every operation, like `momo_common.NewIdleTimeoutConn`) to ensure connections are forcibly closed if the peer goes silent.

## 2026-05-10 - Integer Overflow Handling Edge Cases
**Vulnerability:** A custom integer parsing function (`parsePaddedIntFast`) incorrectly handled `math.MinInt64` (-9223372036854775808), rejecting it due to an imprecise overflow check, and silently wrapped around to a completely different number (e.g. 0) when given an extremely large overflowing value like `-92233720368547758080`.
**Learning:** Naive bounds checks like `res > (1<<63-1)/10` using `int64` are insufficient because the absolute value of `math.MinInt64` cannot be represented in a signed `int64`. Furthermore, if an overflow happens and wraps the accumulator into a negative value, subsequent "greater than" bounds checks might silently pass.
**Prevention:** When writing custom integer parsing functions in Go, ensure comprehensive overflow protection. Accumulate the absolute value using an unsigned integer (`uint64`), dynamically adjust the maximum allowed digit based on the sign to correctly handle two's-complement edge cases, and ensure bounds checks accurately protect against wrap-around.

## 2026-05-11 - DoS via Missing Concurrent Connection Bounds
**Vulnerability:** The server `Accept` loop spawned a new goroutine for every incoming connection without tracking or limiting the total active number. An attacker could open unbounded concurrent connections without completing requests, exhausting server memory, TCP ports, and file descriptors (DoS).
**Learning:** Even simple, fast handlers require a concurrency ceiling. Unbounded goroutine creation in a network loop is a structural vulnerability.
**Prevention:** Implement a maximum concurrent connection limit (e.g., using a buffered channel as a semaphore `sem := make(chan struct{}, maxConns)`) in the `Accept` loop before dispatching connection handlers.

## 2026-05-11 - Silent Reduction of Token Entropy
**Vulnerability:** The application used `momo_common.PadString` to enforce a fixed length for the `AuthToken`. However, `PadString` also silently truncated inputs longer than the target length. This meant administrators providing long, highly secure passwords would unwittingly have their security reduced to the first 64 bytes without any warning or error.
**Learning:** Security configurations must never fail silently by dropping data. Silent truncation converts a secure configuration into a less secure one while maintaining a false sense of security for the operator.
**Prevention:** When validating security inputs (like passwords or tokens) that have a maximum length constraint, always error explicitly if the input exceeds the constraint, rather than silently truncating it.

## 2026-05-11 - Slowloris Bypass via Rolling Timeouts
**Vulnerability:** The application used an `IdleTimeoutConn` that updated `SetReadDeadline` relative to the current time on every successful read. This rolling timeout allows an attacker to upload a large file indefinitely by sending exactly one byte just before the rolling timeout expires, tying up connection slots and memory (Slowloris variant).
**Learning:** Rolling idle timeouts protect against dead peers, but they do not guarantee a maximum transaction time. An active but malicious peer can game rolling timeouts to hold resources forever.
**Prevention:** In addition to rolling idle timeouts, establish a hard, absolute deadline for resource-intensive operations based on logical constraints (e.g., maximum expected duration for a 1GB transfer) and ensure the connection honors the stricter of the two deadlines.

## 2026-05-15 - Missing Audit Logging for Remote Authentication and Configuration Changes
**Vulnerability:** The application handled sensitive operations (authentication failures and cluster replication mode changes) but did not log the IP address or remote peer identifier (`connection.RemoteAddr()`) in the associated warning or audit logs.
**Learning:** Security logs are insufficient if they indicate *what* happened but not *who* did it. Without remote peer identifiers, incident response teams cannot investigate the source of an attack, and automated protections like fail2ban cannot dynamically block brute-force or unauthorized access attempts.
**Prevention:** For all network endpoints enforcing authentication or performing state/configuration changes, log the remote peer identifier (e.g., `connection.RemoteAddr()`) on failure, success, and explicitly prefix sensitive operations with `AUDIT:` to facilitate log ingestion and monitoring.
## 2026-05-19 - Phased Absolute Deadlines for Slowloris Prevention
**Vulnerability:** Even if an absolute deadline is calculated and applied before processing a large payload (like a file transfer), a malicious client can perform a Slowloris-style attack *during* the initial handshake phase (reading authentication tokens, timestamps, metadata) if this phase is only protected by rolling idle timeouts. The client can drip-feed the handshake bytes to tie up the connection handler indefinitely.
**Learning:** Rolling idle timeouts are insufficient for protecting initial handshake parsing logic from trickling connection exhaustion. Absolute deadlines must be applied immediately upon connection acceptance, before reading any data, to bound the duration of the setup phase.
**Prevention:** Apply a short, strict absolute deadline (e.g., 10 seconds) immediately upon accepting the connection or wrapping it, to protect the initial handshake and metadata parsing. Once the handshake is complete and the payload size is known, recalculate and apply a new, dynamically sized absolute deadline for the actual transfer phase.
## 2026-06-03 - Stale Handshake Deadlines causing DoS
**Vulnerability:** The application used an `IdleTimeoutConn` wrapper that amortized `SetDeadline` syscalls by skipping 63 out of 64 calls. When the initial strict handshake deadline was overwritten using `SetAbsoluteDeadline` to allow a long file transfer, the new deadline was not immediately applied to the underlying `net.Conn`. If the subsequent file transfer didn't trigger enough reads/writes quickly enough to force an update, the stale 10-second handshake deadline would expire, prematurely terminating valid file transfers (Denial of Service).
**Learning:** Amortizing syscalls creates a delayed synchronization state between the wrapper's view of the deadline and the kernel/net package's view. Critical deadline updates must bypass amortization to ensure they are immediately enforced by the underlying socket.
**Prevention:** When updating the absolute deadline on an amortized connection wrapper (like `IdleTimeoutConn`), explicitly compute and immediately apply the new deadline directly to the underlying `net.Conn` using `SetDeadline` to ensure stale timeouts are flushed.
## 2026-06-07 - Orphaned Security Sanitization (Path Traversal / DoS)
**Vulnerability:** Path traversal (`../../../file`) and DoS (unbounded `metadata.Size`) vulnerabilities existed because the sanitization logic was implemented in an unused/dead utility function (`getMetadata` in `file.go`), while the active network data path (`comm.ReceiveMetadata()`) returned unsanitized metadata directly to the core `Daemon` handler.
**Learning:** Security checks that are not structurally enforced on the active data paths are ineffective. It's a common pattern for "hardened" functions to be bypassed during refactoring if the architecture allows extracting raw data directly from the connection via alternative methods.
**Prevention:** Enforce input sanitization at the lowest possible layer (e.g., directly inside `ReceiveMetadata` for each protocol) or immediately at the first use site in the handler before *any* logic (like deadline calculations or logging) consumes the untrusted data.

## 2026-06-12 - Path Traversal via Unsanitized Hash Metadata
**Vulnerability:** The application used an unsanitized `metadata.Hash` (derived from client input such as the `X-Amz-Content-Sha256` header) as a component in a file storage path (`storage.CASStore.getBlobPath`). This allowed an attacker to perform directory traversal using payloads like `../../../` as the hash, leading to arbitrary file overwrites and subsequent logic bypasses when the content validation failed post-write.
**Learning:** Security validation must encompass *all* client-provided inputs that influence path construction, not just explicit filenames. Cryptographic hashes, when treated as path components in Content-Addressable Storage (CAS), are just as susceptible to path traversal as filenames if not properly validated.
**Prevention:** Explicitly validate that user-provided hashes conform to expected structural limits (e.g., verifying they do not contain path traversal characters like `.`, `/`, or `\`) immediately upon receipt and before any interaction with the storage tier.

## 2026-07-05 - Path Traversal via Unsanitized Hash string in Networking protocol
**Vulnerability:** The application reads file metadata (`Hash`, `Name`, `Size`) from TCP network connections in `MomoTCPCommunicator` and `getMetadata`, but failed to sanitize the `Hash` string immediately upon receipt. It was possible for an attacker to send path traversal characters (`.`, `/`, `\`) in the `Hash` string to target file storage operations and bypass security checks.
**Learning:** Security controls like path sanitization must be enforced immediately after receiving the input from the network connection, before any processing or state updates occur. The codebase explicitly expects any potential path traversal to be blocked by sanitizing all metadata inputs, including hashes. Automated review checks may flag POSIX syscall mappings (like `syscall.EBADMSG`) as "bad" or "cross-platform incompatible", but in this application's specific context, `syscall` constants are mandated by design for consistent error handling and should be retained over generic errors.
**Prevention:** Explicitly validate that user-provided network metadata strings conform to expected structural limits (e.g., verifying they do not contain path traversal characters like `.`, `/`, or `\`) immediately upon receipt in all protocol communicators and data handlers. When handling protocol errors, ensure standard POSIX error mapping (`syscall`) is utilized to adhere to the project's standardized error handling format.

## 2026-07-17 - Denial of Service (DoS) via Unbounded Slice Memory Allocation
**Vulnerability:** The S3-compatible `HandshakeServer` parsed the `max-keys` query parameter from user input and used it to iterate through metadata elements, generating a potentially massive XML string in a `bytes.Buffer` without enforcing any upper limit on `max-keys`.
**Learning:** Even if `FormatListObjectsV2XML` stops when `keyCount >= maxKeys`, allowing `maxKeys` to be arbitrarily large (e.g., 1 billion) means an attacker can force the server to allocate massive amounts of memory if the database contains millions of objects, leading to OOM crashes.
**Prevention:** To prevent Denial of Service (DoS) via memory or CPU exhaustion, always proactively clamp user-provided limit parameters like `max-keys` to a safe upper bound (e.g., 1000) before loading data or constructing large XML responses.

## 2026-07-22 - DoS via Unbounded MaxKeys in S3 ListObjectsV2
**Vulnerability:** The application correctly checked for length limits on structural components like the bucket name or file hashes in `FormatListObjectsV2XML` to prevent XML generation failures, but it lacked upper-bound limits for the logical configuration variable `max-keys`. A client could request an arbitrarily large `max-keys` (e.g., `1000000`) and the API would attempt to process and construct an XML response containing thousands of metadata entries, causing unbounded CPU cycles and excessive memory allocation per connection (Denial of Service).
**Learning:** Limiting parameter length (e.g., ensuring a string length < 64 bytes) is not sufficient for numeric configuration variables that control the scale of operations (like array sizing or looping limits). Without logical bounds-checking, valid numeric parameters can still be used for resource exhaustion attacks.
**Prevention:** Always implement a strict upper-bound "clamp" limit on parameters that control loop iterations, result sets, or allocations, especially those received from unauthenticated endpoints, before allowing them to influence memory or CPU intensive logic.

## 2026-07-22 - HTTP Request Smuggling (CRLF Injection) in S3 metadata requests
**Vulnerability:** HTTP Request Smuggling (CRLF Injection) in `S3Communicator.SendMetadata`.
**Learning:** When manually constructing raw HTTP requests using byte buffers (e.g., to avoid standard library allocation overhead), you bypass the automatic header and path sanitization provided by `net/http`. If user-controlled input (like a file path or name) is injected directly into the raw request string without validation, an attacker can embed carriage returns (`\r`) and line feeds (`\n`) to inject malicious HTTP headers or forge entirely new requests on the same connection.
**Prevention:** Always explicitly validate that user-controlled inputs do not contain `\r` or `\n` (`strings.ContainsAny(input, "\r\n")`) before appending them to manually constructed raw HTTP request buffers.
