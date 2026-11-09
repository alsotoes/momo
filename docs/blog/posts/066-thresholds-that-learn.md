---
title: "Thresholds That Learn"
date: "2026-11-08T21:34:52Z"
draft: false
post_type: architecture
tags:
  - metrics
  - adaptive
  - control-loop
categories:
  - metrics
summary: "Every control loop that switches replication modes needs a threshold, and every fixed threshold is wrong for someone. This change lets the controller learn the node's own baseline, with hard clamps and a deliberate 5-point dead zone so it cannot oscillate."
artifacts: [{type: spec, path: openspec/changes/adaptive-thresholds-v1}, {type: adr, path: docs/adr/0017-adaptive-thresholds-v1.md}]
related: ["063-gossip-load-without-a-coordinator", "064-failing-over-coordination-in-seconds"]
difficulty: "intermediate"
pattern: "adaptive-control"
teaches:
  - "Why fixed thresholds fail across hardware"
  - "How an EWMA baseline turns into a safe threshold"
  - "Why hysteresis matters more than the algorithm"
---

A control loop that raises and lowers replication pressure needs a number that
says "this node is busy". Pick that number once, in a configuration file, and it
is wrong for almost everyone: 70% memory on a small box is an emergency, on a
large one it is Tuesday. The usual answer is to tell operators to tune it, which
is the same as telling them to discover it by outage.

## Learning the Baseline

The controller already samples memory utilisation every tick. Instead of
comparing that sample to a fixed number, it now keeps a smoothed average of what
normal looks like for this node — an exponentially weighted moving average, with
a small alpha so a spike does not move it.

Once enough samples exist (thirty by default), the upper threshold becomes the
observed baseline plus two standard deviations, plus a small margin. The
configured value stays as a floor: learning may relax the loop in the presence of
a genuinely busy baseline, but it never makes the node tolerate more than the
operator asked for. The lower threshold follows the quiet level instead, so the
gap between "reduce pressure" and "resume" reflects this node's actual range.

## Clamps Before Cleverness

Learned values are dangerous for the same reason they are useful: they follow
reality. A cluster that spends an hour at 94% would happily teach the loop that
94% is normal. The implementation refuses that in four places:

- the learned maximum is clamped to 60–95%;
- the learned minimum is clamped to 20–60%;
- the pair always keeps at least ten points between them, so a mode cannot
  oscillate between two thresholds that touch;
- a change smaller than five points is not applied at all — the control loop only
  moves when the signal has genuinely moved.

That last clause is the one that matters in production. The algorithm is trivial;
the dead zone is what keeps a noisy metric from turning into a replication mode
that flaps every tick. A stable signal is verified to leave the thresholds
exactly where they are.

## Persisting What Was Learned

A restart should not throw the baseline away, so the learned pair is written to a
small BoltDB file in the node's data directory — its own file, not the CAS store's,
because two writers on one bbolt file deadlock. Writes are atomic, the interval is
configurable, and setting it to zero keeps the values in memory for deployments
that prefer a clean slate per boot.

Persistence is deliberately not trusted blindly: values are clamped again on load,
so a hand-edited or corrupted file cannot disable the safety rails.

## The Same Trick on Buffers

Cluster-wide replication broadcasts share one reusable buffer, sized once at 1 KiB.
With more nodes or more metadata per object, nearly every send outgrew it and paid
a reallocation. The pool now feeds each observed payload length into the same kind
of learner and resizes itself to one and a half times the smoothed mean, clamped
between the old 1 KiB floor and 1 MiB. Buffers whose capacity no longer matches the
current target are simply released instead of being returned, so a resize can never
mix sizes inside the pool.

## The Same Idea on Admission and Timeouts

Two more loops in the daemon had fixed numbers that only fit one machine. The
connection admission controller already probed the host at boot — file-descriptor
limit, cgroup or RAM ceiling — but nothing ever fed it pressure afterwards, so the
limit sat at its boot value. It now samples CPU and memory every ten seconds and
sheds to half above 80% and a quarter above 90%: a loaded node stops accepting new
connections instead of accepting them and thrashing.

Metadata RPCs had a flat five-second timeout. On a cluster with millisecond round
trips that is five seconds of waiting to fail over; on a slow link it is a
premature death sentence. The timeout now follows the cluster's measured mean RTT
at ten times that value, clamped between half a second and ten seconds, with the
configured value still winning if an operator sets one.

The pattern is the same every time: measure what the system actually experiences,
derive the number from it, clamp it, and keep a manual override.

## Watching It Work

Three gauges expose the loop's decision — the learned maximum, the learned
minimum, and how many observations fed them. If the sample count stays low, the
node is young and the configured values are in force. If the values move, the
operator can see exactly when and how far, which is the difference between a
self-tuning system and a mysterious one.

## References / Dig deeper

- OpenSpec change: `openspec/changes/adaptive-thresholds-v1/`
- Tracking issue: https://github.com/alsotoes/momo/issues/1145
- Configuration reference: `docs/GUIDES/CONFIGURATION.md` (`[adaptive]`)
