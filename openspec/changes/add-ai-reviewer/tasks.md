## 1. Infrastructure
- [x] 1.1 Add `GEMINI_API_KEY` placeholder in documentation (Secrets requirement).
- [x] 1.2 Create `.github/workflows/gemini_reviewer.yml`.

## 2. Implementation
- [x] 2.1 Develop a script to extract PR context (title, body, diff).
- [x] 2.2 Design a prompt that incorporates `openspec/project.md` steering rules.
- [x] 2.3 Implement the API call to Gemini.
- [x] 2.4 Implement the feedback posting logic using `gh pr comment`.

## 3. Verification
- [ ] 3.1 Test the reviewer on a sample PR with intentional rule violations.
- [ ] 3.2 Ensure it correctly identifies missing `recover()` blocks or `syscall` error mappings.
