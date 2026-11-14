---
title: "A Durability Knob That Finally Counts"
date: "2026-11-13T23:36:10Z"
draft: false
post_type: architecture
tags:
  - durability
  - replication
  - configuration
categories:
  - durability
summary: "`write_quorum` promised that a write is only acknowledged once N replicas have it. Until now the daemon refused any value above 1, because nobody counted replica acknowledgements. The acknowledgements were already arriving on the wire — they just were not being counted."
artifacts: [{type: spec, path: openspec/changes/write-quorum-enforcement}, {type: adr, path: docs/adr/0102-write-quorum-enforcement.md}]
related: ["072-configuration-that-cannot-lie", "070-tenant-quotas", "071-making-refusals-visible", "075-dynamic-replication-order"]
difficulty: "intermediate"
pattern: "acknowledged-replica-quorum"
teaches:
  - "Why counting acknowledgements is enough to enforce a write quorum"
  - "How to tell a quorum that cannot be met from one that is merely slow"
  - "Why a durability knob must name what it counts: replicas, not fsyncs"
---

Replication has two very different promises hiding behind one word. The first is
"the bytes were sent". The second is "the bytes are somewhere else, and that
somewhere has confirmed it". Only the second one survives a machine dying four
seconds later.

The configuration key `write_quorum` has always described the second promise: a
write is acknowledged to the client only after *N* replicas have it. For a while
the daemon refused any value above `1`, with a note pointing at the tracking
issue. The refusal was honest — the setting would have lied — but it meant the
only reachable durability level was "the one node you happened to talk to".

## The Acknowledgements Were Already There

The interesting part of this fix is how little it needed. On the server-side
replication path, the receiving node forwards the blob to its peers with a
streaming sender, and that sender does not return until the peer has answered
with an ACK:

```go
if err := comm.ReceiveACK(); err != nil {
    log.Printf("Failed to read ACK from server: %v", ...)
    return err
}
```

So a forwarder that returns `nil` is not "the bytes were written to a socket".
It is "the peer parsed the metadata, stored the payload, and answered". The
replica count was already being observed — it was being collapsed into a single
boolean (`forwardFailed`), which is enough to avoid silent data loss but throws
away the *number*.

Counting instead of collapsing is the whole change:

```go
var forwardAcks atomic.Int32
markForwardAck := func() { forwardAcks.Add(1) }
```

Each forwarder that succeeds increments it. After the forwarders settle:

```go
acks := 1 + int(forwardAcks.Load())   // the local store is one replica
if q := cfg.Global.WriteQuorum; q > 1 && acks < q {
    log.Printf("AUDIT: Upload from %s reached %d replica(s) but write_quorum requires %d; withholding ACK", ...)
    return // success stays false → no ACK reaches the client
}
```

The client's `ReceiveACK` then fails, and it reports a failed upload. That is the
correct shape of a durability failure: the caller finds out, and can retry while
the data is still in hand.

## A Quorum That Can Never Be Met Is a Config Bug

Once the quorum is enforced, a new configuration mistake becomes possible. The
default `replication_order` mixes modes, but an operator can configure only
client-side modes — `primary-splay`, where the *client* is responsible for
fanning the upload out to its replicas.

That path never collects replica acknowledgements: the client sends to each
replica and logs per-replica failures. With `write_quorum = 2` and nothing but
client-side modes, every single write would fail, forever, no matter how healthy
the cluster is.

A quorum that is structurally unattainable must be rejected where the operator
can still fix it — at configuration load:

```go
if v > 1 && !hasServerSideFanout(globalCfg.ReplicationOrder) {
    return ConfigurationGlobal{}, fmt.Errorf(
        "'write_quorum' > 1 requires a server-side forwarding mode (chain or splay) in 'replication_order'")
}
```

This is the same principle as the original refusal, turned one level finer. The
earlier rule was "do not accept a setting the daemon cannot honour". The refined
rule is "do not accept a combination that cannot be honoured, even though each
half is valid alone".

## Name What You Count

The subtle risk in a durability feature is overclaiming. `write_quorum` counts
**acknowledged replicas** — nodes that confirmed they stored the blob. It does
not count fsyncs. Each node's on-disk durability is set independently by
`[storage] durability`:

- `fsync` — the blob and its directory entry are flushed before the ACK;
- `group-commit` — ACKs are batched behind a periodic barrier;
- `none` — acknowledged with no flush at all, explicitly non-durable.

So `write_quorum = 3` with `durability = none` means "three nodes have the bytes
in their page cache". That is a real improvement over one node — it survives a
single process crash — but it does not survive three simultaneous power losses.
The documentation says this in as many words, because "durable" quietly meaning
two different things is how these guarantees rot.

## What Was Deliberately Left Out

- **Read quorum.** Reads are served from a single replica. Consistency stays
  read-your-writes on one node; a read quorum is a separate design with its own
  repair story.
- **Quorum for client-side replication.** The native client fans out and logs
  failures; making it fail the upload on a short quorum changes the client's
  error contract and deserves its own change.
- **Per-peer quorum accounting in metrics.** The quorum result is visible as a
  withheld ACK plus an error counter. A dedicated "quorum failures" counter
  would be the next observability step.

The pattern is worth keeping in mind: before adding machinery for a promise,
check whether the information is already flowing through the system. Here the
ACKs were already on the wire, counted by nobody. The fix was to stop throwing
the number away.

## References / Dig deeper

- Spec: `openspec/changes/write-quorum-enforcement/`
- ADR: `docs/adr/0102-write-quorum-enforcement.md`
- Tracking issue: [#1340](https://github.com/alsotoes/momo/issues/1340), part of [#1323](https://github.com/alsotoes/momo/issues/1323)
- Code: `src/server/server.go` (ack counting + enforcement), `src/common/config.go` (validation)
- Durability modes: `docs/ARCHITECTURE.md` and `openspec/changes/r3-durability-consistency/`
