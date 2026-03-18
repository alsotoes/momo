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
## $(date +%Y-%m-%d) - Disk Exhaustion DoS via Unbounded Resource Allocation
**Vulnerability:** The server parsed and accepted file sizes up to the maximum capacity of an `int64` without any validation against upper bounds or negative values. This could allow an attacker to send a maliciously large file size, causing the server to exhaust disk space or other resources when attempting to process the file transfer (Disk Exhaustion DoS).
**Learning:** Network endpoints that allocate resources (like file storage) based on client-provided metadata must validate that metadata against strict upper bounds to prevent resource exhaustion attacks.
**Prevention:** Always define and enforce a `MaxFileSize` limit (or similar resource bound) before accepting and processing data streams from untrusted clients. Also check for negative sizes which might cause integer overflow/underflow issues in downstream logic.
