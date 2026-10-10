# No Blog Post Justification: linting-complexity-gates

This change ships CI tooling — a golangci-lint configuration, a lint workflow,
a Makefile target, and the standard they enforce — with no product or
architectural narrative. The engineering decision it encodes (mirror Sonar's
S3776 gate locally with the same metric and threshold, new-code-only) is
captured in `docs/CORE/STANDARDS.md` ("Complexity budget") and in this ADR;
a journal post would restate them without adding a teachable subsystem story.

Consistent with the repo's precedent of `no-blog` for CI/quality tooling
(e.g. the ECC reviewer integration, ADR 0064).

Tracking issue: https://github.com/alsotoes/momo/issues/1172
