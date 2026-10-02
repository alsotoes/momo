# Tasks: Blog Journal Readability (#1116)

## 1. Standard + documentation
- [ ] Update `docs/blog/WRITING_GUIDE.md`: self-contained narrative; references
      only in a final `References / Dig deeper` section; reconcile the DRY clause
- [ ] Update `docs/blog/README.md`: content rules + validation section reflect
      the standard and the new checks

## 2. Enforcement (CI)
- [ ] Extend `.github/scripts/blog_check.py`:
      - [ ] require a `References`/`Dig deeper` heading
      - [ ] require a narrative opening (not a bare changelog list)
      - [ ] flag `openspec/`/`src/`/`docs/`/`.github/` paths in prose outside the
            References section
      - [ ] flag unrendered LaTeX (`$…$`)
- [ ] Wire the checks into the existing `blog-check` make target / workflow

## 3. Rendering fix
- [ ] Convert `$…$` math to plain prose/code in the 7 affected posts
      (001, 002, 013, 019, 020, 031, 043)

## 4. Corpus rewrite (thematic batches)
- [ ] Batch 1: 001–007 (origin / transport / storage foundations)
- [ ] Batch 2: 008–012, 030, 033–036, 039, 040 (S3 gateway)
- [ ] Batch 3: 013–015, 037, 046 (crypto / security / hardening)
- [ ] Batch 4: 016–018 (P2P)
- [ ] Batch 5: 019–021, 043 (durability / self-heal)
- [ ] Batch 6: 022, 023, 029 (momofs)
- [ ] Batch 7: 024, 025, 042, 045, 047, 048, 049 (performance)
- [ ] Batch 8: 026–028, 032, 041, 044 (governance / roadmap / metrics)
- [ ] Each post: explain in prose, define jargon, move repo/spec/doc/PR refs to
      `References`, keep diagrams, verify snippets against `src/` (label as
      simplified where they diverge)

## 5. Verification
- [ ] `make blog-check` passes (all posts compliant)
- [ ] `make diagram-check` passes
- [ ] `make adr-sync-check` passes
- [ ] Hugo build succeeds; Cloudflare deploy succeeds
- [ ] Live site spot-check: posts readable without following references

## 6. Compliance
- [ ] OpenSpec change authored (Rule 73)
- [ ] ADR generated via `make adr-sync` (Rule 77/78)
- [ ] Blog post shipped (Rule 76) or `no-blog` justification
- [ ] PR body includes `Resolves #1116` (Rule 11)
