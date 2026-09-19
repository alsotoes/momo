---
title: "🛡️ Sentinel: Fixing Path Traversal Validation"
date: "2026-09-19"
description: "How we fixed a critical path traversal vulnerability in our transport protocols."
categories: ["Security", "Engineering"]
tags: ["sentinel", "security", "path-traversal", "golang"]
authors: ["Jules"]
artifacts: ["openspec/changes/050-fix-path-traversal-validation"]
---

# Fixing Path Traversal Validation in Transport Protocols

Security is a moving target, and sometimes even explicit validation logic can harbor subtle flaws. In this post, we explore a recent vulnerability discovered in Momo's transport protocols (`momo_tcp.go`, `momo_quic.go`, and `s3_communicator.go`) and how we adopted a more robust approach to path traversal protection.

## The Vulnerability

Our transport protocols receive a `wireName` representing a file path or object key. To prevent path traversal attacks (where an attacker uses `../` to escape the intended directory and access sensitive files), we previously validated the input like this:

```go
for _, part := range strings.Split(wireName, "/") {
    if common.HasPathTraversalChars(part) {
        return 0, fmt.Errorf("path traversal in wireName: %w", syscall.EBADMSG)
    }
}
```

While well-intentioned, this logic is fundamentally flawed:

1.  **Masking Slashes:** By using `strings.Split(wireName, "/")`, we actively strip out all slash characters. The resulting `part` strings will *never* contain a slash. This renders the `HasPathTraversalChars` check (which looks for slashes and `..`) completely blind to absolute path attacks (e.g., `/etc/passwd`).
2.  **False Positives:** If we had simply passed the raw `wireName` directly to `common.HasPathTraversalChars` without splitting, it would falsely reject valid virtual directories (like `user/uploads/image.png`) because it strictly prohibits *any* slash character.

## The Impact

Because the validation logic was circumventable, an attacker could potentially construct malicious payloads that bypass the transport-layer checks. If the backend storage layer failed to enforce its own strict validation, this could lead to unauthorized filesystem writes or reads.

## The Fix

To resolve this, we replaced the custom splitting logic with Go's robust standard library path resolution function, `path.Clean()`.

```go
cleaned := path.Clean(wireName)
if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.HasPrefix(cleaned, "/") {
    return 0, fmt.Errorf("path traversal in wireName: %w", syscall.EBADMSG)
}
```

### Why this works:

1.  **Lexical Resolution:** `path.Clean()` resolves all `.` and `..` elements lexically. If the input is `foo/../bar`, it becomes `bar`. If it attempts to escape the root like `../../etc/passwd`, it resolves to `../etc/passwd`.
2.  **Explicit Checks:** We then explicitly check the cleaned path.
    *   `== "."` or `== ".."`: Rejects attempts to reference the current or parent directory directly.
    *   `strings.HasPrefix(cleaned, "../")`: Rejects any resolved path that attempts to traverse upwards out of the intended root directory.
    *   `strings.HasPrefix(cleaned, "/")`: Rejects absolute paths, ensuring all operations remain relative to the intended virtual root.

This approach correctly allows legitimate virtual directories (like `foo/bar`) while strictly preventing any form of directory traversal or absolute path injection.

## Sentinel Learnings

This fix reinforces a critical security learning: **Do not reinvent path resolution.** String manipulation is error-prone and easily bypassed. Always rely on standard library functions like `path.Clean()` or `filepath.Clean()` to normalize paths *before* applying security constraints.

We have appended this learning to our `.jules/sentinel.md` journal to ensure this pattern is recognized and prevented in future development.
