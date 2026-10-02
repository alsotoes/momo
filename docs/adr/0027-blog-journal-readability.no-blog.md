# No Blog Post Justification: blog-journal-readability

This ADR records a change to the **engineering journal itself** (the writing
standard and its CI validator), not to the momo system. The change is a
documentation/tooling governance decision.

The narrative post that originally accompanied this spec
(`docs/blog/posts/050-making-the-journal-readable.md`) was a **meta/process
post** — a post about the journal — and was removed from the journal, which
chronicles momo's engineering, not its own tooling. The decision remains fully
recorded here (ADR 0027) and in the OpenSpec change
`openspec/changes/blog-journal-readability/`.

No blog post is required per Rule 76: the journal is the engineering record for
the **product**, and a self-referential process note does not belong in it. The
readability standard and its enforcement (`.github/scripts/blog_check.py`) are
already documented for authors in `docs/blog/WRITING_GUIDE.md` and
`docs/blog/README.md`.
