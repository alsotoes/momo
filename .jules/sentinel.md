## 2025-03-13 - Path Traversal bypass with '..'
**Vulnerability:** Path traversal bypassing `filepath.Base()` using a filename of `..` or `.`.
**Learning:** While `filepath.Base()` extracts the last element of a path (e.g. `../../etc/passwd` becomes `passwd`), it specifically returns `..` and `.` when the input is purely those characters. When this is joined with a storage path (e.g. `filepath.Join("/data", "..")`), the resulting path resolves to the parent directory (`/`), escaping the intended sandbox.
**Prevention:** In addition to using `filepath.Base()`, explicitly validate that the resulting filename is not `.`, `..`, `/`, or `\`.

## 2025-03-14 - File Integrity Check Bypass
**Vulnerability:** The application calculates a SHA-256 hash for received files but fails to assert equality with the `expectedHash` before acknowledging the transfer and saving the file.
**Learning:** Computing a security checksum or hash does not provide security unless the value is actively validated against an expected standard and action is taken (like rejecting the file) upon failure. Logging the hash is insufficient for security.
**Prevention:** Always follow checksum computation with an explicit comparison against the expected value and handle mismatches by aborting the operation and cleaning up partial/invalid artifacts.
## $(date +%Y-%m-%d) - Path Traversal bypass via `filepath.Base`
**Vulnerability:** Exact string match checks against `.` or `/` failed to prevent path traversal when embedded within legitimate-looking strings, or across cross-platform boundaries (e.g. `filepath.Base` missing `\` on linux).
**Learning:** `filepath.Base` removes the trailing segments on the *current* operating system, but does not prevent malicious path characters from existing in the middle of a string.
**Prevention:** Always use `strings.Contains` to ensure explicit path separators or traversal strings (`..`) are not embedded anywhere in untrusted filename input.
## 2025-03-19 - Disk Exhaustion DoS via Unbounded Resource Allocation
**Vulnerability:** The server parsed and accepted file sizes up to the maximum capacity of an `int64` without any validation against upper bounds or negative values. This could allow an attacker to send a maliciously large file size, causing the server to exhaust disk space or other resources when attempting to process the file transfer (Disk Exhaustion DoS).
**Learning:** Network endpoints that allocate resources (like file storage) based on client-provided metadata must validate that metadata against strict upper bounds to prevent resource exhaustion attacks.
**Prevention:** Always define and enforce a `MaxFileSize` limit (or similar resource bound) before accepting and processing data streams from untrusted clients. Also check for negative sizes which might cause integer overflow/underflow issues in downstream logic.
## 2025-03-19 - Unbounded resource allocation via `fileSize`
**Vulnerability:** The server blindly trusted the user-provided `fileSize` value in `getMetadata` and used it during memory/disk allocation scenarios (e.g. `io.CopyN`). By sending an extremely large value, an attacker could cause an out-of-bounds allocation or Denial of Service (DoS).
**Learning:** `fileSize` received over the network is entirely untrusted user input, just like `fileName`. Trusting its size directly allows for unbounded allocation vulnerabilities.
**Prevention:** Always validate numeric protocol values like size or length against predefined minimums (0) and maximums (e.g., `MaxFileSize = 1GB`) before acting on them.
## 2025-03-21 - Data Destruction via Insecure File Upload Handling
**Vulnerability:** The server used `os.Create(fullPath)` to write incoming files, immediately truncating any existing file with the same name. If an attacker uploaded a file with the same name as an existing important file and deliberately supplied a bad hash (or dropped the connection), the `defer` block would delete the file or leave it truncated. This is a critical data destruction/DoS vulnerability.
**Learning:** Writing directly to the final destination path before completing all validations (including hash verification and completion checks) exposes existing data to tampering, truncation, or deletion by unauthenticated/unverified inputs.
**Prevention:** Always write uploaded or network-transferred data to a temporary file (`.tmp`). Only after the entire transfer is complete and all security checks (e.g., hash validation) pass, safely commit the file by closing it and using an atomic `os.Rename(tempPath, finalPath)`.

## 2026-03-25 - Unauthenticated file uploads and replication changes
**Vulnerability:** The daemon accepted connections and proceeded to read the file timestamp, metadata and content directly without validating the caller. An authentication handshake was missing.
**Learning:** Accepting network connections and processing data without a verification step (authentication) allows unauthorized users to interact with the system, leading to various security risks including unauthorized data upload and system configuration changes.
**Prevention:** Always implement a mandatory authentication handshake at the beginning of any network communication before processing any protocol-specific data.
## 2026-03-18 - Path Traversal bypass via `filepath.Base`
**Vulnerability:** Exact string match checks against `.` or `/` failed to prevent path traversal when embedded within legitimate-looking strings, or across cross-platform boundaries (e.g. `filepath.Base` missing `\` on linux).
**Learning:** `filepath.Base` removes the trailing segments on the *current* operating system, but does not prevent malicious path characters from existing in the middle of a string.
**Prevention:** Always use `strings.Contains` to ensure explicit path separators or traversal strings (`..`) are not embedded anywhere in untrusted filename input.

## 2026-03-18 - Denial of Service via unbounded memory/disk allocation
**Vulnerability:** Lack of file size validation in metadata retrieval allows attackers to specify extremely large file sizes, leading to resource exhaustion.
**Learning:** Trusting client-provided size metadata without bounds checking can lead to Denial of Service (DoS) when the server attempts to allocate space or process data based on those unvalidated values.
**Prevention:** Always enforce a maximum limit on client-provided numeric values that govern resource allocation, such as file sizes, buffer lengths, or iteration counts.
## 2024-05-24 - Enforce Authentication Handshake
**Vulnerability:** Missing authentication on sensitive endpoints (`Daemon` and `ChangeReplicationModeServer`). Any client could connect and send replication data or files without authorization.
**Learning:** The system architecture lacked a mandatory authentication handshake during the initial connection phase, leaving it open to unauthorized access and potential DoS attacks.
**Prevention:** Always implement an authentication mechanism (e.g., a shared secret or token) for internal services, even if they are not exposed to the public internet. Ensure the token is passed and verified immediately upon connection establishment before processing any further data.
