---
title: "⚡ Bolt: Eliminating Time Formatting Allocations in S3 HTTP Responses"
date: "2026-08-30T11:20:58Z"
draft: false
tags:
  - bolt
  - performance
categories:
  - s3
  - transport
  - performance
summary: "How we optimized S3 XML responses by replacing time.Format with time.AppendFormat to eliminate heap allocations."
artifacts:
  - type: spec
    path: openspec/changes/bolt-s3-copyresult-time-alloc
related: ["024-bolt-performance-engineering"]
---

In our continuous pursuit of performance, we identified a small but frequent allocation in our S3 compatibility layer. When constructing the XML response for `CopyObject`, the system was using `time.Format()` to render the `LastModified` date. While convenient, `time.Format()` dynamically allocates a new string on the heap for every call. In a high-throughput storage system, these small allocations add up, creating unnecessary pressure on the Go garbage collector.

As part of the [⚡ Bolt performance mindset](../../STANDARDS.md) (see docs/STANDARDS.md), we decided to eliminate this overhead.

The solution was straightforward but effective: we switched to using `time.AppendFormat()` alongside a stack-allocated byte array (`var timeBuf [32]byte`). This allows us to write the formatted timestamp directly into the `bytes.Buffer` used for XML construction without any intermediate heap allocations.

### Before:
```go
buf.WriteString(formatLastModified(modTime)) // Internally calls time.Format()
```

### After:
```go
var timeBuf [32]byte
t := time.Unix(0, modTime).UTC()
buf.Write(t.AppendFormat(timeBuf[:0], "2006-01-02T15:04:05.000Z"))
```

Micro-benchmarks confirmed that this simple change completely eliminated string allocations in this hot path, making our S3 responses just a little bit faster and more efficient. Every millisecond counts!