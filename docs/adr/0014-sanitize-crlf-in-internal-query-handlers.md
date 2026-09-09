# 14. Sanitize CRLF in Internal Query Handlers

Date: 2026-09-09

## Status

Accepted

## Context

The internal scatter-gather query handlers (`handleGet`, `handleHas`, `handleDelete`) processed network data (`name` and `hash` fields) without sanitizing for Carriage Return and Line Feed (`\r\n`) characters. While external boundary layers (like S3 communicators) validate for CRLF, internal cluster communications passed via peer-to-peer protocols were not treated with the same defense-in-depth sanitization. Failure to do so could allow protocol smuggling or log injection inside the trusted cluster if a peer node is compromised.

## Decision

We will explicitly validate that network-extracted strings (like names and hashes) do not contain `\r\n` characters immediately upon extraction within internal query handlers (using `strings.ContainsAny`), ensuring strict defense-in-depth even for intra-cluster traffic.

## Consequences

- **Positive:** Prevents protocol smuggling and log injection vulnerabilities from propagating within the internal cluster. Enforces defense-in-depth sanitization across all layers.
- **Negative:** Negligible performance overhead from additional string validation checks on internal query processing.
