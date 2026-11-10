# Tasks — deterministic reviewer gates

- [x] `has_no_blog_justification(pr_body)` — body mention or `docs/adr/*.no-blog.md`; Git failure is not an exemption
- [x] Artifact gates computed once per run (`spec_present`, `blog_present`, `no_blog_exempt`, `is_enhancement`)
- [x] `build_gate_facts(...)` renders the authoritative facts block in the prompt
- [x] Prompt instructs the model not to contradict the facts block
- [x] Tests: `test_ai_reviewer_gates.py` (5 cases); wired into `ai_workflow_check.yml`
- [x] Rule 76: no blog post (CI tooling; see ADR justification)
