> GitHub Issue URL: https://github.com/alsotoes/momo/issues/929

# r1-failure-domains Specification

## Purpose
Make CRUSH replica placement failure-domain-aware so a single-domain outage cannot
destroy all replicas of an object, while preserving deterministic, minimal-movement,
degraded-mode behavior.

## Terminology
- **Failure domain** — an independently failing unit (rack, DC, zone, power domain).
- **Domain set of a placement** — the set of distinct failure domains occupied by the
  selected replicas for an object.

## Configuration

### R1-C1: Node failure domain
- Add `failure_domain string` to per-daemon configuration (`[daemon.N]`).
- Optional; empty means "unclassified" (single default domain). Require `>= 0` domains
  recognized per cluster.

## Placement behavior

### R1-C2: Domain-spread ordering
Given an object hash and `replicationFactor R`, placement MUST pick the top-scoring set
that maximizes the number of distinct failure domains, then (within the same count of
distinct domains) maximizes total CRUSH weight per existing `finalScore` ordering.

Algorithm:
1. Compute per-node `finalScore` (existing WRH).
2. Among candidate replica sets of size R, choose the one with the largest distinct-domain
   count; break ties by descending sum of `finalScore`.
3. Complexity must remain practical (cluster sizes are small; brute-force over
   combinations is acceptable and documented).

### R1-C3: Deterministic + minimal movement
- Placement MUST be deterministic for a fixed topology (same object → same set).
- When topology is unchanged, membership changes MUST NOT remap already-placed replicas
  unless the new replica count requires it (consistent with today's behavior).

### R1-C4: Degraded fallback + warning
- If the domain constraint cannot be satisfied (e.g. `R` > distinct-domain count), CRUSH
  MUST still return `R` replicas but MUST log a warning (extend existing degraded-mode
  logging).
- This mirrors the documented degraded-mode behavior for `replication_factor` under-capacity.

## Tests

### R1-T1
- Table test across same-domain, multi-domain, and partial-domain topologies; assert the
  selected set maximizes distinct domains and matches reference brute-force optimum.
### R1-T2
- Determinism: identical topology + hash yields identical placement across calls.
### R1-T3
- Degraded: `R` > distinct domains logs warning and returns `R` replicas.
### R1-T4
- Goleak + `-race` green; existing CRUSH benchmarks unchanged.
