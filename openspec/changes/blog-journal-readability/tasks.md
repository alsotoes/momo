# Tasks: Blog Journal Readability (#1116)

## 1. Standard + documentation
- [x] Update `docs/blog/WRITING_GUIDE.md`: self-contained narrative; references
      only in a final `References / Dig deeper` section; reconcile the DRY clause
- [x] Update `docs/blog/README.md`: content rules + validation section reflect
      the standard and the new checks

## 2. Enforcement (CI)
- [x] Extend `.github/scripts/blog_check.py`:
      - [x] require a `References`/`Dig deeper` heading
      - [x] require a narrative opening (not a bare changelog list)
      - [x] flag `openspec/`/`src/`/`docs/`/`.github/` paths in prose outside the
            References section
      - [x] flag unrendered LaTeX (`$…$`)
- [x] Wire the checks into the existing `blog-check` make target / workflow
- [x] Flip `BLOG_READABILITY_STRICT` default to enforce (warnings → errors)

## 3. Rendering fix
- [x] Convert `$…$` math to plain prose/code in the 7 affected posts
      (001, 002, 013, 019, 020, 031, 043)

## 4. Corpus rewrite (thematic batches)
- [x] Batch 1: 001–007 (origin / transport / storage foundations)
- [x] Batch 2: 008–012, 030, 033–036, 039, 040 (S3 gateway)
- [x] Batch 3: 013–015, 037, 046 (crypto / security / hardening)
- [x] Batch 4: 016–018 (P2P)
- [x] Batch 5: 019–021, 043 (durability / self-heal)
- [x] Batch 6: 022, 023, 029 (momofs)
- [x] Batch 7: 024, 025, 042, 045, 047, 048, 049 (performance)
- [x] Batch 8: 026–028, 031, 032, 041, 044 (governance / roadmap / metrics)
- [x] Each post: explain in prose, define jargon, move repo/spec/doc/PR refs to
      `References`, keep diagrams, verify snippets against `src/` (label as
      simplified where they diverge)

## 5. Verification
- [x] `make blog-check` passes (all posts compliant)
- [x] `make diagram-check` passes
- [x] `make adr-sync-check` passes
- [x] Hugo build succeeds
- [x] Post 050 and all 48 posts pass `BLOG_READABILITY_STRICT=1`

## 6. Compliance
- [x] OpenSpec change authored (Rule 73)
- [x] ADR generated via `make adr-sync` (Rule 77/78)
- [x] Blog post shipped (Rule 76)
- [x] PR body includes `Resolves #1116` (Rule 11)

## Post-merge (operational, not part of the change)
- [x] Cloudflare Pages deploy of the rewritten journal
- [x] Live-site spot-check at https://momo.apps.headup.ws/
