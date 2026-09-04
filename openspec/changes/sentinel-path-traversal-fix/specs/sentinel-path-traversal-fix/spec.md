# Sentinel: Fix Path Traversal Bypass via Sanitization

Issue: https://github.com/alsotoes/momo/issues/450
Status: Accepted

## Summary
Moved the `common.HasPathTraversalChars` validation to occur immediately on the raw network buffer (`rawHash`), *before* it is passed to `common.SanitizeLog`. This ensures malicious payloads are rejected before they are mutated.
