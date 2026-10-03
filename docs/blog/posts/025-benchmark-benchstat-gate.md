---
title: "The Benchstat Gauntlet: Performance Regressions as CI"
date: 2026-08-28T00:32:50Z
draft: false
post_type: issue
tags: [go, bolt, benchmark, benchstat, ci]
categories: [performance]
summary: "Base-vs-branch benchmark comparison gates every PR — plus the runner-noise saga (S3PutSpool, LocalWrite, allowlist, apostrophe) that made it honest."
artifacts:
  - {type: issue, id: "958"}
  - {type: issue, id: "960"}
  - {type: pr, id: "961"}
  - {type: doc, path: docs/PERFORMANCE.md}
  - {type: spec, path: openspec/changes/steering-three-dot-diff-gate}
related:
  - 024-bolt-performance-engineering
  - 042-perf-profiling-baseline
---
A performance rule with no teeth is a suggestion. We had a pile of
zero-allocation patterns we believed in, and no way to stop the next change from
quietly undoing one. So we built a gate: on every pull request, run the
benchmarks against the base branch and the proposed branch, compare them, and
block the merge if the new code is measurably slower or allocates more.

The comparison tool is `benchstat`, the standard Go program that separates real
performance differences from run-to-run noise. A **regression** is any
statistically significant slowdown or extra allocation introduced by a change. A
**benchmark gate** is the CI rule that turns a regression into a failed build. An
**allowlist** is the short list of benchmarks we tell the gate to ignore, because
we know they measure the environment rather than the code.

That last idea — that some benchmark failures are not about code at all — is the
whole story of August 2026.

## The Real Problem

The gate kept failing for reasons that had **nothing to do with the change under
review**. This is classic benchmark runner noise: the shared CI machine is noisy,
so a benchmark can look 8% slower on one run and 3% faster on the next with no
code difference. A gate that cries wolf is worse than no gate, because people
learn to override it.

Each false alarm forced a fix that made the signal more trustworthy:

1. **A quoting crash.** `benchstat` exited with a crash code because an
   apostrophe inside a comment in the shell allowlist broke the surrounding
   shell quoting. The benchmark data was fine; the *script parsing it* was not.
   The fix was as small as it was embarrassing: no apostrophes in those
   comments.
2. **A disk-spool artifact.** The `S3PutSpool/64MiB` benchmark regressed
   seemingly at random. It was not a code change: the CI virtual machine writes
   its spool to a slow temporary disk, so the benchmark was measuring disk speed,
   not our code. We allowlisted it as an I/O-artifact benchmark.
3. **A local-write artifact.** `LocalWrite` behaved the same way — the runner's
   own filesystem noise. Allowlisted for the same reason.

## Why the Obvious Solutions Failed

**"Just raise the threshold."** Loosening the allowed percentage hides the
environment noise, but it also hides the real regressions we built the gate to
catch. We would have traded a noisy gate for a useless one.

**"Retry until it passes."** Re-running a flaky benchmark until it looks green is
not measurement; it is a coin flip with extra compute. It also trains reviewers
to ignore the gate.

**"Delete the noisy benchmarks."** Tempting, but spool and local-write paths are
exactly the storage behavior we care about. The answer was to keep measuring them
and *label* the noise, not to stop looking.

## The Solution: An Honest, Auditable Gate

Three decisions made the gate trustworthy:

1. **Compare against the base with a three-dot diff.** A three-dot diff compares
   the branch tip against the common ancestor it forked from, so merging the main
   branch into your branch cannot make the comparison look artificially clean.
   The gate measures *your* change, not the merge.
2. **Allowlist environment-sensitive benchmarks explicitly.** Rather than
   silently ignoring failures, the allowlist names the benchmarks that measure
   the runner, and that list is reviewed like code.
3. **Keep an authoritative history.** A generated performance document and a
   checked-in benchmark history file record every run, so a disputed regression
   can be traced over time instead of argued about.

The result distinguishes **real regressions** (blocked) from **environment
jitter** (allowlisted and recorded). The distinction is the entire point: a gate
you trust is a gate people respect.

## ⚡ Bolt Lens

A benchmark gate is Bolt *practiced as process*: measure, compare, gate. The
three-dot and allowlist story is the reminder that **measurement infrastructure
itself needs hardening** — an honest gate is an auditable trust invariant. The
⚡ Bolt mindset supplies the performance bar; the 🛡 Sentinel mindset insists the
bar be enforced by something auditable rather than by good intentions.

See [docs/STANDARDS.md](../../STANDARDS.md) for the ⚡ Bolt / 🛡 Sentinel mindsets.

## References / Dig deeper

- Mindset standards: [docs/STANDARDS.md](../../STANDARDS.md).
- The Bolt engineering post this gate protects: [024](024-bolt-performance-engineering.md).
- Profiling baseline: [042](042-perf-profiling-baseline.md).
- The performance document: `docs/PERFORMANCE.md`; benchmark history:
  `.github/data/benchmark_history.csv`; the gate workflow:
  `.github/workflows/benchmark_compare.yml`.
- The three-dot diff governance spec: `openspec/changes/steering-three-dot-diff-gate`.
- Tracking issues [#958](https://github.com/alsotoes/momo/issues/958),
  [#960](https://github.com/alsotoes/momo/issues/960) and the fix PR
  [#961](https://github.com/alsotoes/momo/pull/961).
