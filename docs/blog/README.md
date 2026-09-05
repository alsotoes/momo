# Momo Engineering Journal (`docs/blog`)

Hugo-format journal covering the project journey: research, architecture
decisions, engineering tradeoffs, and changes — each embedded with the
⚡ Bolt (performance) and 🛡 Sentinel (security) mindsets.

Posts are a **narrative/link layer**. The OpenSpec changes remain the source of
truth for requirements (Rule 39); docs under `docs/` are the reference
material. Posts link to both rather than duplicating them (DRY).

## Post format (Hugo static-site, content-only)

Each post is a markdown file in `posts/` with YAML front matter:

```yaml
---
title: "Post title"
date: 2026-08-11T20:33:28Z
draft: false
tags: [go, cas, crush, bolt]
categories: [storage]
summary: "One-line summary"
artifacts:
  - {type: spec, path: openspec/changes/add-cas-storage}
  - {type: pr, id: "838"}
  - {type: issue, id: "820"}
related:
  - 004-cas-content-addressable-store
---
Body in GitHub-flavored markdown.
```

### Field contract

| Field | Required | Rule |
|---|---|---|
| `title` | yes | Human-readable title |
| `date` | yes | `createdAt` of anchor issue/PR (`gh issue/pr view <N> --json createdAt`) or earliest code/plan commit date when no issue exists. Never future-dated (spec §date) |
| `draft` | yes | `false` for published posts |
| `tags` | yes | Include `bolt` and/or `sentinel` when perf/security drove the design (spec §bolt-sentinel) |
| `categories` | yes | One of: `origin`, `transport`, `storage`, `s3`, `p2p`, `durability`, `encryption`, `momofs`, `performance`, `governance`, `metrics`, `roadmap` |
| `summary` | yes | ≤ 200 chars |
| `artifacts` | yes | openspec change path + PR/issue IDs as evidence |
| `related` | yes | Sibling post filenames (basename in `posts/`) forming cross-links |

## Content rules (spec §implemented-state)

- Posts describe **implemented** behavior, verified against `src/` code and
  docs **outside** `docs/momofs/`.
- `docs/momofs/` is a design/plan suite. Do NOT present its features as shipped
  unless the matching code exists in `src/momofs`. Tag such items `planned`.
- Operational momofs docs (`MOUNT_USER_GUIDE.md`, `IMPLEMENTATION.md §2.3`) are
  usable when ratified by `src/momofs` code.

## Cross-linking

`related` establishes the corpus graph. Add both directions when you author a
post that depends on or explains another (e.g. CRUSH post ↔ CAS post, P2P posts
↔ durability R3 post). Rule: every `related` entry must exist as a file in
`posts/`; the blog CI check fails otherwise.

## Adding a post (Rule 76)

1. OpenSpec change ships (or already shipped) with a matching GitHub issue.
2. Author `posts/NNN-slug.md` per the schema above; `date` from the anchor
   artifact via `gh`, not the commit.
3. Link source artifacts in the body (openspec change dir, PR, issue).
4. Tag ⚡ `bolt` / 🛡 `sentinel` where relevant and link `docs/STANDARDS.md`.
5. Add/refresh `related` in sibling posts.
6. CI (`blog_check.yml`) validates front-matter schema + `related` existence.

## Validation

`make blog-check` (wraps `.github/scripts/blog_check.py`) verifies every post:
required fields, RFC3339 `date` not in the future, `related` targets exist, and
`artifacts: spec` paths resolve under `openspec/changes/`.

## UI/UX Architecture & Skills (`docs/blog/.agents/skills/`)

The blog interface is styled following the **Swiss Modernism 2.0 / Technical Editorial** design system, enforcing rules from `.agents/skills/`:

1. **Accessibility (`accessibility/`)**:
   - WCAG 2.4.1 keyboard skip-to-content link (`layouts/baseof.html`)
   - WCAG 2.4.7 visible focus indicator rings (`:focus-visible`)
   - WCAG 2.3.3 reduced motion overrides (`@media (prefers-reduced-motion: reduce)`)
   - WCAG 2.5.8 touch target scaling (min 44×44px for buttons, pagination, menu)

2. **Frontend Design & Typography (`frontend-design/`, `uiux-designer/`)**:
   - Technical color palette with clean CSS variables (`assets/css/extended/design.css`)
   - Sticky glassmorphism header (`backdrop-filter: blur(14px)`)
   - Interactive article cards with hover lift (`translateY(-2px)`) and accent glow
   - Specialized mindset badges for ⚡ **Bolt** (amber) and 🛡 **Sentinel** (indigo shield)
   - Code block readability: monospace font stack with zero-latency copy button

3. **Search (`/search/`)**:
   - Client-side Fuse.js search (`content/search.md`) indexing all 46 posts via `index.json` output

4. **Cross-Link Resolution**:
   - Custom Hugo render hook (`layouts/_default/_markup/render-link.html`) converts sibling `.md` links to `/posts/<slug>/` and doc links (`../../STANDARDS.md`) to canonical GitHub URLs.

## Cloudflare Pages Deployment

The blog is deployed to Cloudflare Pages via GitHub Actions (`.github/workflows/cloudflare-pages-deploy.yml`).

### Prerequisites

1. **Cloudflare account** with Pages project created (e.g. `momo-app`)
2. **GitHub repository configuration**:
   - Secret `CLOUDFLARE_API_TOKEN` — API token with Pages edit permissions
   - Secret `CLOUDFLARE_ACCOUNT_ID` — Cloudflare account ID
   - Variable `CLOUDFLARE_PAGES_PROJECT` — Cloudflare Pages project name (`momo-app`)

### Deployment trigger

The workflow runs on:
- Push to `master` branch with changes in `docs/blog/**`
- Manual `workflow_dispatch`

### Local preview

```bash
cd docs/blog
hugo server --buildDrafts --buildFuture
# Visit http://localhost:1313
```

### Production URL

The site is deployed to Cloudflare Pages at:
- **Production**: https://momo-app-2r2.pages.dev
- **Preview deployments**: Available on PR preview URLs (e.g. `https://<hash>.momo-app-2r2.pages.dev`)