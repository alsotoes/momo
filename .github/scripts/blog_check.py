#!/usr/bin/env python3
"""Validate docs/blog posts (Rule 76 / blog-posts-hugo spec).

Checks every docs/blog/posts/*.md:
  - YAML front matter with required keys (title, date, draft, tags,
    categories, summary, artifacts, related)
  - date is RFC3339 and NOT in the future (post date = anchor artifact createdAt)
  - every `related` entry resolves to an existing sibling post file
  - every `artifacts: {type: spec, path: ...}` resolves under openspec/changes/

Also enforces Rule 76 COVERAGE: every spec whose ADR (docs/adr/) is "Accepted"
must be referenced by at least one blog post's artifacts, OR have an explicit
`no-blog` justification recorded (either a `no-blog: <reason>` key in the ADR
front matter or a sibling `docs/adr/<id>.no-blog.md` file).

Exit 0 on success, 1 on any violation.
"""
import datetime as dt
import logging
import os
import re
import sys
from pathlib import Path

import yaml

ROOT = Path(__file__).resolve().parents[2]  # repo root (script lives in .github/scripts)
BLOG_DIR = ROOT / "docs" / "blog" / "posts"
SPECS_DIR = ROOT / "openspec" / "changes"

REQUIRED = ("title", "date", "draft", "tags", "categories", "summary", "artifacts", "related")
VALID_CATEGORIES = {
    "origin", "transport", "storage", "s3", "p2p", "durability",
    "encryption", "momofs", "performance", "governance", "metrics", "roadmap",
}
FRONT_MATTER_RE = re.compile(r"^---\r?\n(.*?)\r?\n---\r?\n", re.DOTALL)
FILENAME_PATTERN = re.compile(r"^\d{3}-.+\.md$")

# --- Readability checks (issue #1116) -------------------------------------
# A post must be self-contained: a References/Dig-deeper section is required,
# repository paths must not be used as the explanation in the prose, and no
# unrendered LaTeX. Set BLOG_READABILITY_STRICT=0 to downgrade to warnings.
STRICT_READABILITY = os.environ.get("BLOG_READABILITY_STRICT", "1") != "0"
FENCED_CODE_RE = re.compile(r"^```.*?^```", re.MULTILINE | re.DOTALL)
REFERENCES_HEADING_RE = re.compile(
    r"^#{1,6}\s+.*\b(references|dig deeper)\b", re.IGNORECASE | re.MULTILINE
)
# Repository paths that must not carry the explanation in the narrative body.
REPO_PATH_RE = re.compile(r"(?<![\w/.-])(openspec/|src/|docs/|\.github/)[A-Za-z0-9_./-]*")
# Inline LaTeX `$...$` on one line (the site has no math renderer).
INLINE_MATH_RE = re.compile(r"(?<![\w$])\$[^$\n]{1,120}\$(?![\w$])")
# docs/STANDARDS.md is the required Bolt/Sentinel mindset link; allow it anywhere.
REPO_PATH_ALLOW = ("docs/STANDARDS.md",)
# Known narrative-opening section headings (a lenient signal).
NARRATIVE_HEADING_RE = re.compile(
    r"^#{1,6}\s+.*\b(real problem|the problem|the story|why|context|challenge|background)\b",
    re.IGNORECASE | re.MULTILINE,
)


def parse_front_matter(path: Path) -> tuple[dict, str]:
    text = path.read_text(errors="replace")
    m = FRONT_MATTER_RE.match(text)
    if not m:
        return {}, text
    try:
        fm = yaml.safe_load(m.group(1))
    except yaml.YAMLError as e:
        raise ValueError(f"malformed YAML front matter: {e}") from e
    return (fm or {}), text


def check_post(path: Path, now: dt.datetime, errors: list[str]) -> None:
    if not FILENAME_PATTERN.match(path.name):
        errors.append(
            f"{path.relative_to(ROOT)}: filename must match NNN-slug.md pattern (e.g., 001-origin-and-genesis.md)"
        )

    fm, _ = parse_front_matter(path)
    if not fm:
        errors.append(f"{path.relative_to(ROOT)}: missing YAML front matter")
        return
    for key in REQUIRED:
        if key not in fm:
            errors.append(f"{path.relative_to(ROOT)}: missing required field '{key}'")
        elif fm[key] in (None, "", [], {}):
            errors.append(f"{path.relative_to(ROOT)}: required field '{key}' is empty")

    if "draft" in fm and fm["draft"] is True:
        # drafts are allowed but must still be schema-correct per Rule 76
        pass

    date = fm.get("date")
    if date is not None and date != "":
        try:
            post_date = dt.datetime.fromisoformat(str(date).replace("Z", "+00:00"))
            if not post_date.tzinfo:
                post_date = post_date.replace(tzinfo=dt.timezone.utc)
            if post_date > now:
                errors.append(
                    f"{path.relative_to(ROOT)}: date {date} is in the future "
                    f"(post date must be an anchor artifact createdAt, never future-dated)"
                )
        except (ValueError, TypeError) as e:
            errors.append(f"{path.relative_to(ROOT)}: invalid date '{date}': {e}")

    cats = fm.get("categories", [])
    cats = [cats] if isinstance(cats, str) else cats
    for c in cats:
        if c not in VALID_CATEGORIES:
            errors.append(f"{path.relative_to(ROOT)}: unknown category '{c}'")

    tags = fm.get("tags", [])
    if "bolt" in tags or "sentinel" in tags:
        # Bolt/Sentinel mindset posts should link STANDARDS.md somewhere in body
        if "docs/STANDARDS.md" not in path.read_text():
            errors.append(f"{path.relative_to(ROOT)}: tagged bolt/sentinel but body misses docs/STANDARDS.md link")

    related = fm.get("related", [])
    for rel in related:
        target = BLOG_DIR / f"{rel}.md"
        if not target.exists():
            errors.append(
                f"{path.relative_to(ROOT)}: related '{rel}' has no posts/{rel}.md sibling"
            )

    artifacts = fm.get("artifacts", [])
    for art in artifacts:
        if not isinstance(art, dict):
            continue
        if art.get("type") == "spec" and art.get("path"):
            spec_path = (ROOT / art["path"]).resolve()
            if not spec_path.exists():
                errors.append(f"{path.relative_to(ROOT)}: artifacts spece path '{art['path']}' does not exist")


def check_readability(path: Path, text: str, problems: list[str]) -> None:
    """Self-contained-narrative checks (issue #1116).

    Appends human-readable problems to ``problems`` (the caller routes them to
    errors or warnings depending on STRICT_READABILITY).
    """
    rel = path.relative_to(ROOT)
    m = FRONT_MATTER_RE.match(text)
    body = text[m.end():] if m else text

    # Strip fenced code blocks before scanning prose so code examples don't
    # trip the path/math checks.
    prose = FENCED_CODE_RE.sub("", body)

    # 1. References / Dig deeper section required.
    ref_match = REFERENCES_HEADING_RE.search(prose)
    if not ref_match:
        problems.append(f"{rel}: missing a 'References'/'Dig deeper' section")

    # 2. Narrative opening: first content line is prose (not a list/table/code/
    #    heading) OR the first heading is a known narrative heading.
    content_lines = [ln for ln in body.splitlines() if ln.strip()]
    if content_lines:
        first = content_lines[0].lstrip()
        is_prose = not first.startswith(("#", "-", "*", ">", "|", "```", "1.", "2."))
        if not is_prose and not NARRATIVE_HEADING_RE.search(body):
            problems.append(
                f"{rel}: no narrative opening (first content line is a list/table/heading)"
            )

    # 3. Repository paths must not carry the explanation outside References.
    narrative = prose[: ref_match.start()] if ref_match else prose
    for pm in REPO_PATH_RE.finditer(narrative):
        token = pm.group(0)
        if any(token.startswith(allowed) for allowed in REPO_PATH_ALLOW):
            continue
        problems.append(
            f"{rel}: repository path '{token}' used in the narrative body "
            f"(move it to the References section)"
        )

    # 4. No unrendered LaTeX.
    for mm in INLINE_MATH_RE.finditer(prose):
        problems.append(
            f"{rel}: unrendered LaTeX '{mm.group(0)}' (site has no math renderer)"
        )


def check_coverage(errors: list[str]) -> None:
    """Rule 76 coverage: every Accepted-ADR spec must have a blog post (or no-blog justification)."""
    adr_dir = ROOT / "docs" / "adr"
    if not adr_dir.is_dir():
        return

    # Map every spec ID referenced by any post's artifacts (type: spec).
    covered_specs: set[str] = set()
    for post in sorted(BLOG_DIR.glob("*.md")):
        fm, _ = parse_front_matter(post)
        for art in fm.get("artifacts", []) or []:
            if isinstance(art, dict) and art.get("type") == "spec" and art.get("path"):
                p = art["path"].strip().rstrip("/")
                if p.startswith("openspec/changes/"):
                    covered_specs.add(Path(p).name)

    # Accepted ADRs mark the spec as ratified → requires a post (or no-blog).
    for adr in sorted(adr_dir.glob("*.md")):
        fm, text = parse_front_matter(adr)
        status = None
        m = re.search(r"^## Status\s*\n\s*(Accepted|Proposed|Deprecated)", text, re.M)
        if m:
            status = m.group(1)
        if status != "Accepted":
            continue

        # ADR filename is NNNN-<spec-id>.md → spec id is everything after the first '-'.
        stem = adr.stem
        spec_id = stem.split("-", 1)[1] if "-" in stem else stem

        if spec_id in covered_specs:
            continue
        # no-blog justification: ADR front-matter key or sibling .no-blog.md file.
        if fm.get("no-blog"):
            continue
        if (adr_dir / f"{stem}.no-blog.md").exists():
            continue
        errors.append(
            f"{adr.relative_to(ROOT)}: Accepted spec '{spec_id}' has no matching blog post "
            f"and no no-blog justification (Rule 76 coverage)"
        )


def main() -> int:
    logging.basicConfig(level=logging.INFO)
    posts = sorted(BLOG_DIR.glob("*.md"))
    if not posts:
        logging.error("no posts found under %s", BLOG_DIR)
        return 1

    now = dt.datetime.now(dt.timezone.utc)
    errors: list[str] = []
    readability: list[str] = []
    for post in posts:
        try:
            check_post(post, now, errors)
        except ValueError as e:
            errors.append(f"{post.relative_to(ROOT)}: {e}")
        try:
            check_readability(post, post.read_text(errors="replace"), readability)
        except Exception as e:  # never let a readability check crash the gate
            errors.append(f"{post.relative_to(ROOT)}: readability check error: {e}")

    # related back-references: every post named in some other post's related
    # list must itself declare that post (bidirectional cross-link rule)
    fm_map: dict[Path, dict] = {}
    for post in posts:
        fm_map[post], _ = parse_front_matter(post)
    for post, fm in fm_map.items():
        for rel in fm.get("related", []):
            target = BLOG_DIR / f"{rel}.md"
            if target.exists() and post.stem not in fm_map.get(target, {}).get("related", []):
                errors.append(
                    f"{post.relative_to(ROOT)}: one-way related '{rel}' — "
                    f"{target.name} should list '{post.stem}' back (bidirectional)"
                )

    check_coverage(errors)

    if STRICT_READABILITY:
        errors.extend(readability)
    elif readability:
        logging.warning(
            "readability: %d issue(s) across posts (non-blocking; set "
            "BLOG_READABILITY_STRICT=1 to enforce)",
            len(readability),
        )
        for r in readability[:20]:
            logging.warning("  ⚠ %s", r)
        if len(readability) > 20:
            logging.warning("  … and %d more", len(readability) - 20)

    if errors:
        for e in errors:
            logging.error("  ✗ %s", e)
        logging.error("blog validation failed (%d problems)", len(errors))
        return 1
    logging.info("✓ all %d blog posts valid", len(posts))
    return 0


if __name__ == "__main__":
    sys.exit(main())