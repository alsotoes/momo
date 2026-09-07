---
title: "Transport Evolution: TCP, QUIC, and the Wire Protocol"
date: 2026-08-11T04:02:50Z
draft: false
tags: [go, transport, quic, tcp, bolt]
categories: [transport]
summary: "Momo's transport layer grew from TCP to a QUIC/TLS 1.3 fan-out — fixing handshake, ACK framing, deadlines, and TLS identity along the way."
artifacts:
  - {type: pr, id: "763"}
  - {type: pr, id: "818"}
  - {type: spec, path: openspec/changes/add-quic-protocol}
  - {type: doc, path: docs/PROTOCOL.md}
related:
  - 002-replication-strategies-polymorphic
  - 011-s3-https-tls-enforcement
  - 024-bolt-performance-engineering
---
# Transport Evolution: TCP, QUIC, and the Wire Protocol

Momo ships two transports over one logical wire protocol (`docs/PROTOCOL.md`):
legacy **TCP** (`momo-tcp`) and **QUIC/TLS 1.3** (`momo-quic`). The split was
an explicitly measured bet: TCP for high-bandwidth LAN chains, QUIC for
lossy/WAN fan-out where it avoids head-of-line blocking and gives 0-RTT.

## What the transport had to get right

A decade of fixes concentrated on correctness at the socket boundary:

- **Handshake** — challenge-response auth with a *pre-padded* token, enforced
  freshness (later a plaintext timestamp check + revocation).
- **ACK framing** — a fragile 5ms timeout ACK was replaced with a fixed-length
  4-byte ACK so partial reads could not desync the stream.
- **QUIC listener identity** — the daemon previously ignored its configured TLS
  cert and CA pool (#763); the QUIC server now binds the configured certificate
  face, not a self-signed default.
- **Idempotent close** — QUIC `Close()` once spawned a fire-and-forget goroutine
  with a magic delay; that became a `sync.Once` (#818).
- **Read deadlines** — `IdleTimeoutConn` rolls a deadline so idle sockets cannot
  stall the daemon (a direct seed of the **Bolt** deadline-amortization work).

## ⚡ Bolt lens

The single most Bolt-visible artifact here is **bitwise deadline amortization**:
`SetDeadline` syscalls were cut ~98% in hot paths by computing deadline windows
in bulk instead of per-op — a forward reference to
[024](024-bolt-performance-engineering.md). The same pool/buffer principles
apply to the transfer payload ring.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## Related

Chained reads: [002](002-replication-strategies-polymorphic.md) →
[011](011-s3-https-tls-enforcement.md) → [024](024-bolt-performance-engineering.md).