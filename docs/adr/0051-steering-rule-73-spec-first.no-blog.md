# No Blog Post Justification: steering-rule-73-spec-first

This ADR records a **process/governance rule** — the spec-first implementation
mandate (every feature authors an OpenSpec change before implementation). It is
not an engineering decision about how momo is built.

The engineering journal documents momo's **product engineering** (storage,
replication, S3, crypto, P2P, momofs, performance). A self-referential post
about our own review workflow does not belong in it. The rule itself remains the
source of truth in `openspec/config.yaml` and is explained to contributors in
`docs/AI_FLYING_SOLO.md`.

No blog post is required per Rule 76: the journal is the engineering record for
the product, and a process note about the review workflow is not a product
dispatch. (Its narrative post `027-governance-ai-review-spec-first` was removed
from the journal for this reason.)
