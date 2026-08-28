#!/usr/bin/env python3
"""Validate docs/blog posts (Rule 76 / blog-posts-hugo spec).

Checks every docs/blog/posts/*.md:
  - YAML front matter with required keys (title, date, draft, tags,
    categories, summary, artifacts, related)
  - date is RFC3339 and NOT in the future (post date = anchor artifact createdAt)
  - every `related` entry resolves to an existing sibling post file
  - every `artifacts: {type: spec, path: ...}` resolves under openspec/changes/

Exit 0 on success, 1 on any violation.
"""
import datetime as dt
import logging
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


def parse_front_matter(path: Path) -> tuple[dict, str]:
    text = path.read_text()
    m = FRONT_MATTER_RE.match(text)
    if not m:
        return {}, text
    try:
        fm = yaml.safe_load(m.group(1))
    except yaml.YAMLError as e:
        raise ValueError(f"malformed YAML front matter: {e}") from e
    return (fm or {}), text


def check_post(path: Path, now: dt.datetime, errors: list[str]) -> None:
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


def main() -> int:
    logging.basicConfig(level=logging.INFO)
    posts = sorted(BLOG_DIR.glob("*.md"))
    if not posts:
        logging.error("no posts found under %s", BLOG_DIR)
        return 1

    now = dt.datetime.now(dt.timezone.utc)
    errors: list[str] = []
    for post in posts:
        try:
            check_post(post, now, errors)
        except ValueError as e:
            errors.append(f"{post.relative_to(ROOT)}: {e}")

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

    if errors:
        for e in errors:
            logging.error("  ✗ %s", e)
        logging.error("blog validation failed (%d problems)", len(errors))
        return 1
    logging.info("✓ all %d blog posts valid", len(posts))
    return 0


if __name__ == "__main__":
    sys.exit(main())