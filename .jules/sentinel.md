## 2025-03-13 - Path Traversal bypass with '..'
**Vulnerability:** Path traversal bypassing `filepath.Base()` using a filename of `..` or `.`.
**Learning:** While `filepath.Base()` extracts the last element of a path (e.g. `../../etc/passwd` becomes `passwd`), it specifically returns `..` and `.` when the input is purely those characters. When this is joined with a storage path (e.g. `filepath.Join("/data", "..")`), the resulting path resolves to the parent directory (`/`), escaping the intended sandbox.
**Prevention:** In addition to using `filepath.Base()`, explicitly validate that the resulting filename is not `.`, `..`, `/`, or `\`.

## $(date +%Y-%m-%d) - Path Traversal bypass via `filepath.Base`
**Vulnerability:** Exact string match checks against `.` or `/` failed to prevent path traversal when embedded within legitimate-looking strings, or across cross-platform boundaries (e.g. `filepath.Base` missing `\` on linux).
**Learning:** `filepath.Base` removes the trailing segments on the *current* operating system, but does not prevent malicious path characters from existing in the middle of a string.
**Prevention:** Always use `strings.Contains` to ensure explicit path separators or traversal strings (`..`) are not embedded anywhere in untrusted filename input.
