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
## 2024-05-24 - Timing Attack on AuthToken Validation
**Vulnerability:** The server used standard Go string comparison `string(bufferAuthToken) != cfg.Global.AuthToken` to validate the client's authentication token. This exposed the system to timing attacks, where an attacker could deduce the correct token length and contents by measuring response times.
**Learning:** Even simple string equality checks in security contexts can be dangerous in Go. Additionally, when using `subtle.ConstantTimeCompare`, both byte slices must be exactly the same length or the function will immediately return 0, which still leaks length information.
**Prevention:** Always use `crypto/subtle.ConstantTimeCompare` for verifying cryptographic secrets and tokens. Ensure the expected token is properly padded to match the protocol's fixed length before comparison.
## 2026-04-25 - Denial of Service via Unhandled Accept Errors
**Vulnerability:** The daemon `Accept()` loops in `src/server/server.go` and `src/server/replication.go` called `os.Exit(1)` when encountering an error. Transient errors, such as `EMFILE` (too many open files), could thus crash the entire server, creating a severe Denial of Service (DoS) vulnerability.
**Learning:** Network listener loops must be resilient against transient connection errors. Exiting the process on `Accept()` failure allows trivial resource exhaustion attacks to take down the service.
**Prevention:** Always log the error, sleep briefly (e.g., `time.Sleep(10 * time.Millisecond)`) to prevent CPU spinning on persistent errors like `EMFILE`, and `continue` the loop rather than crashing the process.
