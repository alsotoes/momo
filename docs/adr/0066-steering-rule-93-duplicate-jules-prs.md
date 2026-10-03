# 0066-steering-rule-93-duplicate-jules-prs

## Status
Accepted

## Confidence
High

## Context
Jules — the automated Bolt/Sentinel agent — opens **more than one PR and branch
for the same task** across parallel runs. During the aws-chunked zero-allocation
work, two PRs shipped the identical optimization:

- **#1112** "⚡ Bolt: Zero-allocation string parsing for AWS chunked headers"
  (merged as `60933b80`).
- **#1114** "⚡ Bolt: Eliminate heap allocations in parseAWSChunkHeader"
  (duplicate; same `parseAWSChunkHeader` change).

Each carried its own auto-trace issue (#1113, #1115) and its own stale branch.
After #1112 merged, #1114 sat open with a redundant tracker until manually
closed. The working tree also kept a stale remote branch
(`bolt/aws-chunked-zero-allocation-…`) that had to be pruned.

The existing rules do not cover this case:

- **Rule 80** (Jules PR takeover/rebuild) handles **one** PR that needs a clean
  rebuild — not two PRs doing the same thing.
- **Rule 90** (auto-trace dedup) makes the **reviewer** stop creating duplicate
  *issues* for one PR — it says nothing about duplicate *PRs*.

Without a rule, duplicates linger: the tracker shows two "open" efforts for one
task, CI runs twice, and a reader cannot tell which is canonical.

## Decision


## Consequences


## Alternatives Considered
None documented.

## Implementation Status
- **Code**: Done
- **Tests**: Done
- **Docs**: Planned
- **Blog post**: 

## References
- Issue: #1121
- PR: 
- Spec: `openspec/changes/steering-rule-93-duplicate-jules-prs/`
- Blog: 

