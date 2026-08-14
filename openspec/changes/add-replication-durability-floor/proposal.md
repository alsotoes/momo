# Proposal: Replication Durability Floor Under Load

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/822

- **Champion:** opencode (deepseek-v4-flash-free)
- **Status:** `Draft`

## 1. Problem

Momo's metrics-driven controller (`src/metrics/metrics.go`) dynamically moves
the active replication mode along the configured `replication_order` based on
CPU / memory load (`checkMetricsAndSwap`) and a timeout fallback. On a busy or
loaded node it **counts memory/CPU pressure as a reason to persist fewer durable
copies** — for example it may choose `ReplicationNone` (no replica copies) or a
lower-tier mode even when the operator explicitly configured a minimum
`replication_factor`. This can silently forfeit durability exactly when the
cluster is under load:

- the controller's mode selection is decoupled from the operator's durability
  intent, and
- the transition is "silent" — a degraded mode is selected without any
  indication that the desired durability floor is no longer being met.

## 2. Proposed Solution

Introduce an explicit, operator-configured **durability floor** and enforce it
in the metrics controller so the active mode never drops below a mode that can
still hold at least the configured minimum number of replica copies.

### 2.1 Config: `minimum_durability_factor`

Add `MinimumDurabilityFactor` to `ConfigurationGlobal`, parsed from
`[global] minimum_durability_factor` in `loadGlobalConfig`. Semantics:

- `0` (default) → no floor, controller behaves exactly as today.
- `N >= 1` → the controller may **not** select an active mode whose achievable
  replica count is less than `N`.

The achievable replica count of a mode is computed as:

- `ReplicationNone` → **1** (the source copy only; no replicas).
- `ReplicationChain` / `ReplicationSplay` / `ReplicationPrimarySplay` →
  `min(ReplicationFactor, number_of_daemons)` (bounded by cluster size).

Validation: `minimum_durability_factor` must be `>= 0` and, when > 0, it must
be `<= replication_factor` and, for replicated modes, reachable given the
daemon count (else a boot-time warning, deferred to spec).

### 2.2 Controller enforcement

In `GetMetrics` (`src/metrics/metrics.go`):

1. After `checkMetricsAndSwap` proposes a **lower**-durability mode (degrade),
   compute the floor value of the proposed mode; if it is below the configured
   minimum, **do not switch** — remain on the current, higher-durability mode and
   log a warning (Rule 9 — no silent failures): the controller refuses to
   silently drop below the floor.
2. Apply the same guard to the **timeout fallback** degrade path.
3. Mode **escalation** (raising durability) is never blocked by the floor.

The guard lives in the metrics controller because that is the only code path
that *automatically* changes the mode under load; the change-replication control
channel (`replication.go`) remains operator-driven and is unaffected.

## 3. Wire & Protocol Impact

None. No handshake bytes, mode codes, or payloads change (Rules 7, 38). The
floor is purely a controller-side admission rule.

## 4. Testing

- Unit tests for `effectiveDurability(mode, factor, clusterSize)`: None=1,
  replicated=min(factor, clusterSize), and the floor guard: proposed-degrade
  blocked when below floor; upgrade allowed; floor unset ⇒ no change.
- Config parse tests: default 0, parsed value, invalid (negative / too-large)
  rejected with `EINVAL`.
- Integration-style test of `GetMetrics` degrade path simulating load and
  asserting the mode is held at the floor.

## 5. Backward Compatibility

Default `minimum_durability_factor = 0` is a no-op — all existing behavior and
tests are unchanged. When enabled, only the load-driven **degrade** path can be
held at the floor (never blocks the operator driving modes via the control
channel). Complies with Rules 4 (bounded, no new unbounded state), 5 (tests),
7 (wire stability), 9 (no silent failures — logs the hold), 27 (docs).
