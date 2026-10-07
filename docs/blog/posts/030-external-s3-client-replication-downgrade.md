---
title: "External S3 Client Replication Downgrade: When aws-cli Can't Fan Out"
date: 2026-07-01T23:36:39Z
draft: false
post_type: issue
tags: [s3, replication, bolt, sentinel]
categories: [s3]
summary: "External S3 clients like aws-cli don't send momo headers — the server detects this and downgrades client-side replication modes to server-side alternatives, ensuring replication never silently drops."
artifacts:
  - {type: spec, path: openspec/changes/add-external-client-replication}
  - {type: issue, id: "258"}
related:
  - 016-p2p-gossip-swim
  - 018-adaptive-scaling-peer-quality
---
An `aws-cli` upload and a momo-client upload look almost identical on the wire:
both are S3 `PUT` requests. They are not, however, equally capable of keeping your
data safe. momo can replicate a write several ways, and two of those ways put the
fan-out work on the *client*. A tool like `aws-cli`, `rclone`, or `boto3` speaks
only the standard S3 API, so it cannot do that fan-out. When one of these
"external" clients hit a node whose active replication mode assumed a momo-aware
client, the server did the worst possible thing: it stored a single copy and
reported success.

## The Real Problem

*Replication* is keeping multiple copies of each object on different machines;
the *replication factor* is how many copies. In momo, a write can be replicated
in one of four shapes. Two of them are **client-side**: the client opens the
parallel connections to every replica itself, so the server relays no bytes. The
other two are **server-side**: the receiving node fans the write out to its peers.

An external S3 client can only ever send one stream. It has no idea which nodes
hold replicas and no protocol to reach them. So a client-side mode is simply not
available to it.

The failure was in how the server told the two client kinds apart. momo clients
add a private header naming the mode they intend, plus a timestamp header. The
server used those to decide whether a request came from a momo client or from a
peer forwarding a write. An external client sends neither, but it *does* send the
standard `X-Amz-Date` timestamp. Parsing that succeeded, and because the value was
not the sentinel "dummy" timestamp used for forwarded traffic, the server
concluded it was a peer forward and applied **no replication at all**. A single
copy landed, with a `200 OK` on top.

## Why the Obvious Fixes Failed

**"Treat a missing header as an error."** That would reject every legitimate
external client — the exact compatibility we were trying to keep.

**"Make external clients speak the momo handshake."** We do not control `aws-cli`,
`rclone`, or the thousands of S3 SDKs. Compatibility means meeting them where
they are.

**"Just force server-side replication globally."** That would throw away
client-side replication for momo clients, giving up the zero-relay-bandwidth win
for the traffic that can actually use it. The mode must depend on *who is asking*,
not on the cluster's global setting.

## The Solution: Per-Request Downgrade

We taught the server to recognize a request that has no momo mode header and
treat it, correctly, as an external client. Then, instead of refusing the write,
it steps the mode down to the next server-side alternative.

A new config key lists which modes are client-side and therefore unusable by an
external client:

```ini
# momo.conf
# Modes that require the client to fan out to replicas. An external
# S3 client cannot do this, so the server downgrades past them.
client_side_replication_modes = 3
```

When an external request arrives:

1. **Detect** — no momo mode header means external client; the timestamp is
   forced to the sentinel value so the peer-forward path is never taken.
2. **Downgrade** — walk the configured replication order forward to the first
   server-side mode (for example, client-side primary-splay to server-side splay).
3. **Keep it local** — the downgrade is per transaction. The cluster's global
   mode is untouched, so momo clients on the same node still get client-side
   replication.
4. **Stay configurable** — if we ever add another client-side mode, we add it to
   the config list; no code change.

The key design choice is *per-transaction*. A global mutation would have been
simpler to write but would have flipped the mode for concurrent momo clients
mid-flight. Because the decision is computed per request, two clients with
different capabilities can share one node and each get the right behavior.

## How We Verified

- An `aws-cli` `PUT` to a node running client-side primary-splay downgrades to
  server-side splay and the object is replicated.
- A momo client using the sentinel timestamp keeps client-side primary-splay
  unchanged.
- Concurrent momo and external clients each get their correct mode while the
  global mode stays at primary-splay.
- Config validation defaults the client-side mode list to primary-splay.

## What Could Go Wrong

| Risk | Guard |
|------|-------|
| A future client-side mode added without updating config | The downgrade walk is driven entirely by the config list, so an unlisted mode would be treated as server-side; the list is validated at startup |
| Misidentifying a peer forward as an external client | The sentinel-timestamp rule is only applied when the momo mode header is absent, preserving the peer path |
| Global mode accidentally mutated | The downgrade returns a per-request value; the stored cluster mode is never written on this path |

## When NOT to Use This

- **momo-aware clients.** They should keep client-side replication; the downgrade
  only triggers when the mode header is missing.
- **Clusters with a single node.** With no replicas there is nothing to downgrade
  to, and the mode is irrelevant.

## Engineering Standards (⚡ Bolt & 🛡 Sentinel)

Per [docs/CORE/STANDARDS.md](../../STANDARDS.md):

- ⚡ **Bolt**: the config list is parsed with the same zero-allocation CSV parser
  used elsewhere, and the downgrade is a single integer walk — no allocation on
  the request path.
- 🛡 **Sentinel**: fail-closed. A missing mode header is treated as the least
  capable client, never as permission to skip replication, so the silent
  single-copy outcome cannot recur.

## References / Dig deeper

- Spec: `openspec/changes/add-external-client-replication/`.
- Issue: #258.
- Related posts: [016: P2P Gossip and SWIM](016-p2p-gossip-swim.md),
  [018: Adaptive Scaling and Peer Quality](018-adaptive-scaling-peer-quality.md).
- Replication strategies: [002](002-replication-strategies-polymorphic.md).
