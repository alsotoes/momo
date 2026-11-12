---
title: "Dynamic Replication Order — Let the Cluster Pick Its Own Mode"
date: "2026-11-11T16:00:00Z"
draft: false
post_type: architecture
tags:
  - replication
  - adaptive
  - stigmergy
  - operations
categories:
  - metrics
summary: "momo's polymorphic controller picked its replication mode from a static config list, so a poorly performing mode stayed in the sequence forever. Dynamic replication order observes each mode's real performance and re-ranks the sequence so better modes are tried first — with the durability floor and semantic constraints still enforced."
artifacts: [{type: spec, path: openspec/changes/dynamic-replication-order-v2}, {type: adr, path: docs/adr/0075-dynamic-replication-order-v2.md}]
related: ["072-configuration-that-cannot-lie", "073-tenancy-you-can-write-down", "074-when-rotation-does-nothing", "064-failing-over-coordination-in-seconds"]
difficulty: "intermediate"
pattern: "stigmergy"
teaches:
  - "why a static replication order is a hidden reliability tax"
  - "how EWMA scoring + a stability window re-ranks modes without thrashing"
---

Every momo node runs a polymorphic replication controller: when load or memory crosses a
threshold, it degrades (None → Chain → Splay → PrimarySplay) or promotes back up. The order
it walks that path came straight from `config.toml` — static, global, never questioned.

That works until it doesn't. A mode that's slow on your network stays ahead of a faster one.
Chain replication padds along while Splay would have drained the backlog. And because the
order never changes, nobody notices until a pager goes off at 3 a.m.

## The Real Problem

The controller's degradation/promotion path is only as good as its ordering. A static order
means:

- Poorly performing modes stay in the sequence, getting retried every tick.
- Well-adapted modes can't be promoted ahead of worse ones.
- There is no per-workload or per-cluster optimization — every cluster gets the same list.

That's a hidden reliability tax: the system *can* recover, but it recovers along the wrong
path.

## Why the Obvious Solutions Failed

**Reorder manually after an incident.** Operators already have enough to do during an
outage; asking them to edit `config.toml` and restart daemons to "try Splay first" is not
an operational strategy. It's also slow — by the time the order changes, the incident is
over.

**Use a central coordinator to pick the mode.** That's exactly what the distributed
coordination work (#1144) was designed to avoid: a single point of failure, and a round trip
to decide something the local metrics already tell us.

**Promote on every threshold crossing.** Without a stability window, the order would flip-flop
as noise crosses margins — thrashing that makes the cluster less predictable, not more.

## The Solution — With Code Walkthrough

Dynamic replication order (#1146): observe each mode's real performance, score it, and
re-ranks the sequence so better modes are tried first. Disabled by default
(`replication_order.enabled=false`), so it is a zero behavioral change until you opt in.

### The tracker

The tracker keeps an exponentially-weighted moving average of each
mode's success rate, latency, throughput, and error rate:

```go
type ModeResult struct {
	Success   bool
	LatencyNs int64
	Bytes     int64
}

func (t *Tracker) Observe(mode int, result ModeResult)
func (t *Tracker) Order() []int
```

Each observation updates EWMA-smoothed stats (`alpha` configurable, default 0.2), and a
composite score ranks the modes:

```
Score = 0.4*success_rate + 0.3*(1/latency) + 0.2*throughput − 0.1*error_rate
```

Components are normalized to [0, 1] so the weights are directly comparable.

### The stability window

A stability window wraps the tracker (default 10
minutes): the order is only recomputed when the window has elapsed. A single bad sample
cannot flip the sequence; the controller converges gradually.

### Semantic constraints (the part that must never be wrong)

`ReplicationNone` means no replication at all. It must never be tried before `Chain` or
`Splay`, because degrading to None silently sacrifices durability. `Tracker.Order()` applies
this constraint after scoring, so no score — no matter how high — can put None first.

### Wiring into the controller

`GetMetrics` builds a tracker when enabled, refreshes `c.order`
from `tracker.Order()` each tick, and observes push success/failure after every `switchTo`:

```go
func (c *metricsController) switchTo(idx int) {
	c.currentIndex = idx
	mode := c.order[idx]
	persistMode(c.order, idx, c.store)
	failures := pushNewReplicationMode(c.cfg, c.authToken, mode)
	c.observeMode(mode, failures == 0)
	c.start = time.Now()
}
```

`pushNewReplicationMode` now returns the number of peers the broadcast failed to reach, so
the tracker observes real broadcast health, not just "the controller tried."

The durability floor (`MinimumDurabilityFactor`) still applies unchanged: `shouldSwitchMode`
refuses any move to a mode whose achievable replica count falls below the floor, so dynamic
reordering can never silently drop durability.

## The Principle

**Stigmergy: let local evidence produce global order.** Instead of a central coordinator
deciding the replication mode sequence, each node observes its own broadcasts and re-ranks
locally. No new single point of failure, no round trip, no operator in the loop.

The corollary: a re-ranking mechanism is only safe if it cannot violate the invariants the
static order protected. Here that's the durability floor and the "None never first"
constraint — enforced in code, not convention.

## How We Verified

- `go build ./...` clean across all 9 modules; `go vet`/`gofmt -l` clean.
- `go test -race` on the tracker, reorderer, and metrics packages — 64 tests pass, including the
  tracker EWMA decay, the None-never-before-replicated constraint, and the stability window.
- `make lint` (gocognit/gocyclo new-code-only) reports 0 issues.
- `make test` — all 9 modules pass.
- Backward compatibility: with `replication_order.enabled=false` (the default), the static
  config order is used verbatim — no tracking, no reordering, identical behavior.

## What Could Go Wrong

**The tracker only observes mode switches, not steady-state health.** If the order is good,
few switches happen, so few observations are collected — the order stabilizes, which is
desirable, but a slowly degrading mode that never triggers a switch won't be demoted until
something forces a switch. Latency/throughput observation (not yet measured in Phase 1) is
the natural next refinement.

**A mode with a brief outage gets demoted, then the outage ends — but the score remembers.**
EWMA decay handles this over time, but the window means recovery isn't instant. That's the
stability window doing its job: better slow and correct than fast and flip-flopping.

**Reordering changes `currentIndex` semantics.** The controller clamps `currentIndex` to the
new order length each tick, and persistence is mode-based (not index-based), so a restarted
controller resumes the right mode even after the order changed.

## When NOT to Use This

If your cluster has a stable, well-understood workload and you already know which mode is
best, pin it statically with `replication_order.enabled=false`. Dynamic order is for
clusters where the "best" mode shifts with load, network conditions, or data shape — and
where you'd rather the system adapt than page you.

## References / Dig deeper

- OpenSpec change: `openspec/changes/dynamic-replication-order-v2/`
- ADR: `docs/adr/0075-dynamic-replication-order-v2.md`
- Tracker: `src/replication_order/tracker.go`
- Reorderer: `src/replication_order/reorder.go`
- Controller integration: `src/metrics/metrics.go`
- Broadcast health: `src/metrics/replication.go`
- Tracking issue: [#1146](https://github.com/alsotoes/momo/issues/1146)
