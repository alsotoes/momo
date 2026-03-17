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
