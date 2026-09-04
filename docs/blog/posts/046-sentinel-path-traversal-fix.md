---
title: "🛡️ Sentinel: Fix Path Traversal Bypass via Sanitization"
date: 2024-05-24T12:00:00Z
draft: false
description: "Addressing a critical path traversal vulnerability caused by late validation."
tags: ["security", "sentinel"]
artifacts:
  - PR 100
related: []
---

## Vulnerability

Path traversal checks were being performed on network buffers *after* they had been passed through sanitization functions (like `common.SanitizeLog()`). Sanitization functions alter or remove control characters (including null bytes) and potentially path traversal sequences. If the raw input contained path traversal sequences, `SanitizeLog` might alter them, allowing the subsequent path traversal check to pass and admitting a malicious (but altered) payload.

## Prevention

Always perform validation checks (like `HasPathTraversalChars`) on the raw, unsanitized input *before* passing it to logging or sanitization functions.

## Fix

Moved the `common.HasPathTraversalChars` validation to occur immediately on the raw network buffer (`rawHash`), *before* it is passed to `common.SanitizeLog`. This ensures malicious payloads are rejected before they are mutated.
