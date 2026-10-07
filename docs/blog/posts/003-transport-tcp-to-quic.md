---
title: "Transport Evolution: TCP, QUIC, and the Wire Protocol"
date: 2026-08-11T04:02:50Z
draft: false
post_type: architecture
tags: [go, transport, quic, tcp, bolt]
categories: [transport]
summary: "Momo's transport layer grew from TCP to a QUIC/TLS 1.3 fan-out — fixing handshake, ACK framing, deadlines, and TLS identity along the way."
artifacts:
  - {type: pr, id: "763"}
  - {type: pr, id: "818"}
  - {type: spec, path: openspec/changes/add-quic-protocol}
  - {type: doc, path: docs/REFERENCE/PROTOCOL.md}
related:
  - 002-replication-strategies-polymorphic
  - 011-s3-https-tls-enforcement
  - 024-bolt-performance-engineering
---
momo ships two transports over one logical wire protocol: legacy **TCP** and
**QUIC over TLS 1.3**. TCP is the mature, universally available path; QUIC is
the newer one. QUIC runs over UDP and multiplexes many independent streams, so a
lost packet on one stream no longer stalls the others — the problem TCP suffers
from known as *head-of-line blocking*, where one delayed segment blocks every
byte behind it. QUIC also offers *0-RTT*: a returning client can send data in its
very first flight, skipping a round trip of connection setup.

The split was an explicitly measured bet: TCP for high-bandwidth LAN chains,
QUIC for lossy wide-area fan-out where its stream independence and fast
reconnection pay off.

## What the transport had to get right

{{< diagram src="/diagrams/07-transport-tcp-quic.svg" alt="Transport evolution: TCP vs QUIC" caption="Transport evolution: TCP vs QUIC" >}}

A decade of fixes concentrated on correctness at the socket boundary:

- **Handshake.** Challenge-response authentication with a *pre-padded* token,
  so the reply always occupies a fixed size and leaks no length information,
  plus enforced freshness (later reduced to a plaintext timestamp check with
  revocation).
- **ACK framing.** A fragile acknowledgement scheme that relied on a 5 ms
  timeout was replaced with a fixed-length 4-byte acknowledgement. The old
  design could desync the stream when a read returned a partial frame; a
  fixed-size frame cannot.
- **QUIC listener identity.** The daemon previously ignored its configured TLS
  certificate and CA pool, so a QUIC listener presented a self-signed default
  instead of the operator's certificate. The QUIC server now binds the
  configured certificate, matching the TCP/TLS path.
- **Idempotent close.** QUIC `Close()` once spawned a fire-and-forget goroutine
  with a magic delay; it is now guarded by a `sync.Once`, so repeated closes are
  safe and immediate.
- **Read deadlines.** An `IdleTimeoutConn` wrapper rolls a read deadline so idle
  sockets cannot stall the daemon forever. This was a direct seed of the
  **Bolt** deadline-amortization work described below.

## ⚡ Bolt lens

The single most Bolt-visible artifact here is **bitwise deadline
amortization**: `SetDeadline` syscalls were cut by roughly 98% in hot paths by
computing deadline windows in bulk instead of per-operation. The same
pooling-and-buffer principles apply to the transfer payload ring. The full story
is in [024](024-bolt-performance-engineering.md).

See [docs/CORE/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Wire protocol doc: [docs/REFERENCE/PROTOCOL.md](../../PROTOCOL.md).
- Spec: `openspec/changes/add-quic-protocol`.
- Pull requests: #763 (QUIC listener identity), #818 (idempotent close).
- Chained reads: [002](002-replication-strategies-polymorphic.md) →
  [011](011-s3-https-tls-enforcement.md) → [024](024-bolt-performance-engineering.md).
