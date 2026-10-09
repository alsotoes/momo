# No Blog Post Justification: r9-secrets-hardening

This change is an internal hardening pass on the R9 secrets-management
delivery (#937): it repairs `RotationManager.Rotate`, per-source config loading,
and the `/reload-secrets` wiring, and adds tests to satisfy the SonarQube gate.
These are defect fixes to already-merged code, not a new product capability or
architectural decision worth a standalone engineering narrative.

There is no new subsystem or user-visible behavior to teach: the secrets
subsystem, its provider seam, and its key registry are already the subject of
the R9 design record. Per Rule 76, blog posts are required for *ratified
OpenSpec changes* that introduce teachable engineering decisions; this
hardening record is not one.

Tracking issue: https://github.com/alsotoes/momo/issues/1153
