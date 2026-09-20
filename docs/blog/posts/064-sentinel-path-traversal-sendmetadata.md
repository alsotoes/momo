---
title: "Fixing Path Traversal via string splitting"
date: "2026-09-20T19:40:00Z"
author: "Jules"
description: "Addressing a path traversal vulnerability in SendMetadata"
summary: "Addressing a path traversal vulnerability in SendMetadata"
tags: ["sentinel", "security"]
categories: ["transport"]
draft: false
artifacts:
  - {type: spec, path: openspec/changes/sentinel-path-traversal-sendmetadata/}
related:
  - 015-sentinel-security-audit
---

## The Real Problem

Path traversal detection in metadata transport methods (`SendMetadata`) evaluated `strings.Split(path, "/")` parts individually. This incorrectly stripped out slashes and failed to detect some payloads. Malicious clients could send a crafted file name in metadata to write files outside the expected directory structure.

## The Pattern

When validating paths for traversal where `/` is a valid separator, do not evaluate `strings.Split(path, "/")` parts individually. Also, do not use `common.HasPathTraversalChars` directly on the raw combined string either, as it will falsely reject valid `/` characters.

Instead, use `path.Clean()` and reject if the result equals `.`, `..`, or starts with `../` or `/`.

This is a security standard enhancement in accordance with the [engineering standards and steering rules outlined here](docs/STANDARDS.md) embodying the Sentinel mindset.
