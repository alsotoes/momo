# Deterministic artifact gates in the AI reviewer

> GitHub Issue URL: https://github.com/alsotoes/momo/issues/1305

## Why

The AI reviewer computes artifact presence from the repository, but the model
writing the review prose has repeatedly contradicted those computations: on PR
#1304 it claimed the ADR, the `no-blog` justification, and the `Resolves #1303`
link were all missing while every one of them was in the diff and the PR body. Each
false claim blocked the merge and cost a re-review cycle.

## What Changes

- The Rule 76 exemption is computed deterministically: either the PR body mentions
  `no-blog`, or the branch ships a `docs/adr/*.no-blog.md` justification file.
- The artifact gates are computed once per run and rendered into the prompt as an
  authoritative facts block, with an instruction not to contradict it.
- A unit-tested helper (`build_gate_facts`) keeps the prompt block and the tests in
  sync; a new test file covers the exemption and the facts rendering.

No merge-gate semantics change: the deterministic gates remain authoritative and
the model still reviews the diff content.
