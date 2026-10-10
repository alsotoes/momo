# No Blog Post Justification: harness-manifest

This change ships repo tooling — a YAML manifest (`openspec/ai-workflow.yaml`),
a validation script, and a CI gate — with no product or architectural
narrative. The decision it encodes (declare every agent/plugin once, validate
pinning/policy/rule-links in CI) is captured in this ADR and the OpenSpec
change; a journal post would restate them.

Consistent with the repo's `no-blog` precedent for CI/quality tooling
(e.g. ADR 0064 ECC reviewer integration, ADR 0081 linting gates).

Tracking issue: https://github.com/alsotoes/momo/issues/1174
