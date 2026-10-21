---
title: "⚡ Bolt: Zero-Allocation SigV4 Canonical Headers"
date: "2026-10-15T11:43:50Z"
draft: false
post_type: issue
tags:
  - bolt
  - performance
  - s3
  - security
categories:
  - transport
  - performance
summary: "Every authenticated S3 request rebuilds a SigV4 canonical-headers block — and the two obvious ways to write it (strings.Split and Header.Get) each allocate on the hottest authorization path. This post walks through replacing both with an IndexByte scan and an EqualFold lookup, and the golden-equivalence test that proves the rewrite is byte-identical for every request AWS clients actually send."
artifacts:
  - type: spec
    path: openspec/changes/bolt-sigv4-zero-alloc-headers
  - type: issue
    id: 1195
related: ["024-bolt-performance-engineering", "049-eliminate-redundant-time-format-allocations", "051-bolt-aws-chunked-zero-alloc", "052-bolt-etag-matches-zero-alloc"]
difficulty: "intermediate"
pattern: "zero-alloc"
teaches:
  - "SigV4 canonical request construction"
  - "strings.IndexByte vs strings.Split"
  - "why Header.Get allocates"
  - "golden-equivalence testing for auth-critical rewrites"
---

Every request to the S3-compatible gateway that carries a signature goes through AWS Signature Version 4 verification. Part of that dance is building the *canonical request* — a normalized string that both the client and the server must produce identically, byte for byte, or the signature will not match. One of its components is the **canonical headers** block: a sorted, newline-separated list of `name:value` pairs for the headers the client chose to sign.

This post is about removing two heap allocations from that construction — and, more importantly, about how to prove an optimization on authentication code is safe.

## The Real Problem: allocations on the authorization path

The canonical-headers builder did two innocuous-looking things:

1. It split the `SignedHeaders` list (`host;x-amz-content-sha256;x-amz-date`) with `strings.Split`.
2. For each name, it fetched the value with `req.Header.Get(name)`.

Both allocate. `strings.Split` allocates a slice header plus a backing array to hold the parts. And `Header.Get` is not a plain map lookup: it first canonicalizes the key through `net/textproto.CanonicalMIMEHeaderKey`, which allocates a copy of the name string on every call.

This runs on **every authenticated request** — the busiest authorization path in the gateway. Two allocations per request is not catastrophic, but it is pure, avoidable GC pressure exactly where latency matters, and it is the kind of thing the project's performance standard forbids on hot paths.

## Why the Obvious Fix Is Not Enough

The tempting move is "just cache it" or "only optimize if a benchmark says so". Neither is right here.

Caching a per-request canonical string buys nothing — each request has different headers, so there is no reuse to exploit. And optimizing blind is dangerous: this function sits on the *authentication* path, so a subtle behavior change does not show up as a slow request, it shows up as rejected valid signatures or, worse, accepted invalid ones. Performance work on auth code must be **provably behavior-preserving**, not just faster.

## The Solution: two allocation-free primitives

Replace each allocating call with a scan over the data you already have.

**Parsing the signed-header list** — walk the string with `strings.IndexByte`, slicing the original rather than building new strings:

```go
func parseSignedHeaders(s string, buf []string) ([]string, error) {
    headerNames := buf[:0] // reuse a caller-provided stack buffer
    for {
        idx := strings.IndexByte(s, ';')
        if idx == -1 {
            if len(s) > 0 {
                if len(headerNames) >= cap(buf) {
                    return nil, syscall.EBADMSG // bounded: fail closed
                }
                headerNames = append(headerNames, s)
            }
            break
        }
        if part := s[:idx]; len(part) > 0 {
            if len(headerNames) >= cap(buf) {
                return nil, syscall.EBADMSG
            }
            headerNames = append(headerNames, part)
        }
        s = s[idx+1:]
    }
    return headerNames, nil
}
```

The caller passes a stack-allocated `[16]string` buffer, so the common case never touches the heap. The capacity check is a defensive bound: a malicious client cannot force an unbounded list.

**Looking up a header value** — iterate the header map with a case-insensitive comparison instead of asking `Header.Get` to canonicalize the key:

```go
func getCanonicalHeaderValue(req *http.Request, h string) (string, bool) {
    if h == "host" {
        return req.Host, true // Go keeps Host out of the header map
    }
    for k, v := range req.Header {
        if strings.EqualFold(k, h) {
            if len(v) > 0 {
                return v[0], true
            }
            return "", true
        }
    }
    return "", false
}
```

`EqualFold` compares without allocating, and the first-value semantics match what `Header.Get` returned.

> **Principle:** on a hot path, prefer scanning the bytes you already own over calling a helper that builds a new string. `IndexByte` and `EqualFold` read; `Split` and `CanonicalMIMEHeaderKey` write.

## Verification: the golden test

A faster function that produces a *different* string is a security bug, not an optimization. So the rewrite ships with a **golden-equivalence oracle**: a verbatim copy of the old implementation kept in the test file, and a table of conformant inputs asserting the new implementation's output is byte-identical.

```go
want := legacyBuildCanonicalHeaders(req, tc.signedHeaders)
got, err := buildCanonicalHeaders(req, tc.signedHeaders)
if got != want {
    t.Fatalf("canonical output diverged:\n  legacy: %q\n  optimized: %q", want, got)
}
```

The oracle immediately caught a real divergence. For the input `SignedHeaders: "Host;X-Amz-Date"` (note the capital `H`), the old code emitted `Host:\n` — an **empty host value**, because its host special-case only matched the exact lowercase `"host"`. That is a broken canonical request. The new code fails closed with an error instead. Since AWS mandates lowercase signed-header names, that input never occurs in practice — but the test now documents the difference explicitly rather than hiding it.

Two intentional, fail-closed divergences are pinned by dedicated tests:

- a signed header **absent** from the request now returns an error instead of silently contributing an empty value;
- **empty segments** in a malformed list (`host;;x-amz-date`) are skipped instead of emitting a stray `:\n`.

## Failure Modes

- **Over-optimizing the auth path.** If the golden test had been skipped, the capital-`H` divergence could have shipped unnoticed. On auth code, equivalence proof is not optional.
- **Stack-buffer overflow.** A fixed `[16]string` buffer means an oversized list must be *rejected*, not truncated — hence the explicit capacity check returning an error.
- **Assuming `Host` lives in the header map.** Go promotes the request's host into a separate field; the lookup has to special-case it, exactly as before.

## When NOT to Use This

This pattern pays off on per-request hot paths with bounded input sizes. It is not worth it for code that runs once at startup, for lists whose size is genuinely unbounded (you would need a heap buffer anyway), or where the input is already a slice and `Split` would not allocate a new backing array. Reach for `IndexByte`/`EqualFold` when the data is already a string you can slice and the call count is high.

## References / Dig deeper

- The performance standard: [docs/CORE/STANDARDS.md](../../STANDARDS.md)
- Related Bolt posts: [024](024-bolt-performance-engineering.md) the performance
  mindset, [049](049-eliminate-redundant-time-format-allocations.md) SigV4
  datestamp formatting, [051](051-bolt-aws-chunked-zero-alloc.md) aws-chunked
  parsing, [052](052-bolt-etag-matches-zero-alloc.md) ETag parsing.
- Tracking issue: [#1195](https://github.com/alsotoes/momo/issues/1195).
