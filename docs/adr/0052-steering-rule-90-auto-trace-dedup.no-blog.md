# No Blog Post Justification: steering-rule-90-auto-trace-dedup

This ADR records a fix to the **AI reviewer tooling** — it stopped the reviewer
from creating duplicate auto-trace issues for one PR. It is a change to our
governance automation, not an engineering decision about how momo is built.

The engineering journal documents momo's **product engineering**. A post about
our own reviewer script does not belong in it. The fix remains documented in the
steering rule (`openspec/config.yaml`, Rule 90) and the reviewer code.

No blog post is required per Rule 76: the journal is the engineering record for
the product. (Its narrative post `046-auto-trace-dedup` was removed from the
journal for this reason.)
