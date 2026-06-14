# Proposal: Implement Gemini AI Code Reviewer

**Related Issue:** https://github.com/alsotoes/momo/issues/156

- **Champion:** Gemini CLI
- **Status:** `Draft`

## 1. Problem

As a single-contributor project, `momo` lacks a second pair of eyes to catch edge cases, enforce steering rules (Zero-Crash, POSIX Error Mapping, etc.), and maintain architectural consistency. Manual self-reviews are prone to bias and fatigue.

## 2. Proposed Solution

Integrate a Gemini-powered automated reviewer into the GitHub Actions pipeline. This reviewer will act as the automated enforcement arm for the **Gemini CLI & Jules** collaboration protocol. It will:
1.  Analyze the diff of every Pull Request.
2.  Assert adherence to Project Steering Rules defined in `openspec/project.md`.
3.  Specifically verify compliance with the **⚡ Bolt** (performance) and **🛡️ Sentinel** (security) patterns defined in `.jules/bolt.md` and `.jules/sentinel.md`.
4.  Identify potential security vulnerabilities (e.g., CRLF injection, path traversal).
5.  Suggest performance optimizations (zero-allocation patterns).
6.  Post feedback directly as comments on the Pull Request.

## 3. Technical Spec

### GitHub Action
- **Trigger:** `pull_request` (opened, synchronize).
- **Environment:** Ubuntu runner.
- **Authentication:** Requires a `GEMINI_API_KEY` stored in GitHub Secrets.

### Review Logic
- Use a Go script to extract the PR diff using `git diff`.
- Construct a prompt containing the **Project Steering Rules** and the **PR Diff**.
- Call the Gemini API (`gemini-1.5-flash`).
- Use the GitHub CLI (`gh`) to post the generated review.

## 4. Performance Analysis & Justification

- **Overhead:** Minimal. The review happens in parallel with build/test jobs and does not block developer local cycles.
- **Cost:** Uses the developer's Gemini API tier.
- **Reliability:** Provides a consistent automated check for known project mandates.

---
