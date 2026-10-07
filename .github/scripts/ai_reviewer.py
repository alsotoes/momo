import os
import subprocess
import json
import http.client
import sys
import re

# ECC Tools PR-audit comment marker (issue #1134):
#   <!-- ecc-tools:pr-audit:ECC Tools / <dimension>:<sha> -->
ECC_AUDIT_MARKER_RE = re.compile(
    r"<!--\s*ecc-tools:pr-audit:ECC Tools\s*/\s*([^:>]+?):([0-9a-zA-Z]+)\s*-->"
)

def _ecc_status_emoji(status):
    return {
        "success": "✅",
        "neutral": "⚠️",
        "warning": "⚠️",
        "failure": "❌",
        "action_required": "❌",
        "error": "❌",
    }.get((status or "").lower(), "•")


def parse_ecc_audit_summary(comments):
    """Pure parser: given a list of PR comment dicts, return the advisory ECC
    audit summary (latest verdict per dimension), or "" if none are present."""
    latest = {}  # dimension -> (createdAt, status, phrase)
    for c in comments or []:
        login = (c.get("author") or {}).get("login", "")
        if "ecc" not in login.lower():
            continue
        body = c.get("body", "")
        m = ECC_AUDIT_MARKER_RE.search(body)
        if not m:
            continue
        dimension = m.group(1).strip()
        verdict = re.search(r"\*\*(.+?)\*\*\s*\(([a-z_]+)\)", body)
        if verdict:
            phrase, status = verdict.group(1).strip(), verdict.group(2).strip()
        else:
            phrase, status = "reported", ""
        created = c.get("createdAt", "")
        prev = latest.get(dimension)
        if prev is None or created > prev[0]:
            latest[dimension] = (created, status, phrase)

    if not latest:
        return ""

    order = [
        "Security Evidence",
        "PR Risk Taxonomy",
        "Reference Set Readiness",
        "Hosted Promotion Readiness",
    ]
    dims = [d for d in order if d in latest] + [d for d in latest if d not in order]
    parts = [f"{d}: {_ecc_status_emoji(latest[d][1])} {latest[d][2]}" for d in dims]
    return "**ECC audit** (advisory) — " + " · ".join(parts)


def get_ecc_audit_summary(pr_number):
    """Read the ECC Tools PR-audit comments and return a compact advisory summary
    of the latest verdict per audit dimension (issue #1134).

    Returns "" when there are no ECC comments or on any error. Advisory only:
    never influences the approve/merge decision.
    """
    if not pr_number:
        return ""
    try:
        cmd = ["gh", "pr", "view", str(pr_number), "--json", "comments"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        comments = json.loads(result.stdout).get("comments", [])
    except Exception as e:
        print(f"ECC audit lookup skipped: {e}", file=sys.stderr)
        return ""
    return parse_ecc_audit_summary(comments)


# ── REST-based PR/issue mutations (issue #1140) ──────────────────────────────
# `gh pr edit` / `gh issue edit` run GraphQL queries that request org-scoped
# fields (login/name/slug) and therefore fail when the token only has the
# `repo` scope:
#   GraphQL: The 'login' field requires ['read:org'], but your token has only
#   been granted: ['repo']
# Every label/assignee/body mutation silently failed as a result. These helpers
# use the REST API instead, which the `repo` scope covers.

def _repo_slug():
    return os.environ.get("GITHUB_REPOSITORY", "alsotoes/momo")


def _gh_api(method, path, payload=None):
    """Call `gh api` with an optional JSON body. Returns the CompletedProcess."""
    cmd = ["gh", "api", "-X", method, path]
    if payload is not None:
        cmd += ["--input", "-"]
        return subprocess.run(cmd, input=json.dumps(payload),
                              capture_output=True, text=True, check=True)
    return subprocess.run(cmd, capture_output=True, text=True, check=True)


def add_pr_labels(pr_number, labels):
    """Add labels to a PR via REST (PR labels live on the issues endpoint)."""
    labels = sorted({l for l in (labels or []) if l})
    if not pr_number or not labels:
        return
    try:
        _gh_api("POST", f"repos/{_repo_slug()}/issues/{pr_number}/labels",
                {"labels": labels})
        print(f"Added labels to PR #{pr_number}: {labels}")
    except Exception as e:
        print(f"Failed to add labels {labels} to PR #{pr_number}: {e}", file=sys.stderr)


def assign_pr(pr_number, user):
    """Assign a user to a PR via REST."""
    if not pr_number or not user:
        return
    try:
        _gh_api("POST", f"repos/{_repo_slug()}/issues/{pr_number}/assignees",
                {"assignees": [user]})
        print(f"Assigned {user} to PR #{pr_number}")
    except Exception as e:
        print(f"Failed to assign {user} to PR #{pr_number}: {e}", file=sys.stderr)


def set_pr_body(pr_number, body):
    """Set the PR description via REST."""
    if not pr_number:
        return
    try:
        _gh_api("PATCH", f"repos/{_repo_slug()}/pulls/{pr_number}", {"body": body})
        print(f"Updated body of PR #{pr_number}")
    except Exception as e:
        print(f"Failed to update body of PR #{pr_number}: {e}", file=sys.stderr)


def create_issue_rest(title, body, labels, assignee):
    """Create an issue via REST. Returns the issue number, or None."""
    payload = {"title": title, "body": body, "labels": sorted(set(labels or []))}
    if assignee:
        payload["assignees"] = [assignee]
    try:
        result = _gh_api("POST", f"repos/{_repo_slug()}/issues", payload)
        return json.loads(result.stdout).get("number")
    except Exception as e:
        print(f"Failed to create issue: {e}", file=sys.stderr)
        return None


def review_is_approved(review):
    """Return True if the reviewer's text signals approval (issue #1140).

    The old check required the review to START with "✅". Jules reviews are
    prefixed with "@google-labs-jules" and/or structured headings, so approved
    Jules PRs never auto-merged. Detect the approval marker robustly, ignore any
    appended ECC advisory block, and never treat an explicit blocker as approval.
    """
    if not review:
        return False
    # Drop any appended ECC advisory block (added after approval is computed).
    core = review.split("**ECC audit**")[0]
    # A blocker is a line that STARTS with a warning marker (optionally after a
    # bullet/quote). Merely mentioning the markers in prose (e.g. describing the
    # rules) must NOT block approval.
    if re.search(r"(?m)^[\s\-\*>]*[🚨🛑]", core):
        return False
    # Strip a leading Jules mention so a prefixed "@google-labs-jules" does not
    # hide the approval marker.
    core = re.sub(r"^\s*@google-labs-jules\b[^\n]*\n+", "", core.strip()).strip()
    return core.startswith("✅") or "✅ All Project Steering Rules" in core


def get_filtered_diff():
    # 🛡️ Rule 15: Strictly limit diff size to prevent token exhaustion.
    max_diff_lines = 1000
    try:
        # Only compare against origin/master
        cmd = ["git", "diff", "origin/master...HEAD", "--", ".", ":(exclude)vendor/*", ":(exclude)go.sum", ":(exclude)go.mod", ":(exclude)docs/*"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        diff = result.stdout
        
        if not diff:
            return None

        lines = diff.splitlines()
        if len(lines) > max_diff_lines:
            return "\n".join(lines[:max_diff_lines]) + f"\n\n🛑 DIFF TRUNCATED AT {max_diff_lines} LINES FOR TOKEN EFFICIENCY (RULE 15)"
        return diff
    except subprocess.CalledProcessError as e:
        print(f"Error getting git diff: {e}", file=sys.stderr)
        return None

def call_gemini(api_key, model, prompt):
    host = "generativelanguage.googleapis.com"
    endpoint = f"/v1beta/models/{model}:generateContent?key={api_key}"
    
    payload = {
        "contents": [
            {
                "parts": [{"text": prompt}]
            }
        ]
    }
    
    headers = {"Content-Type": "application/json"}
    
    conn = http.client.HTTPSConnection(host)
    conn.request("POST", endpoint, body=json.dumps(payload), headers=headers)
    
    response = conn.getresponse()
    if response.status != 200:
        print(f"API Error ({response.status}): {response.read().decode()}", file=sys.stderr)
        return None
        
    data = json.loads(response.read().decode())
    conn.close()
    
    try:
        return data['candidates'][0]['content']['parts'][0]['text']
    except (KeyError, IndexError):
        return "No review generated."

def get_jules_commit_count():
    try:
        # Count commits authored by google-labs-jules in this PR branch
        cmd = ["git", "log", "origin/master..HEAD", "--author=jules", "--oneline"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        lines = result.stdout.strip().splitlines()
        return len(lines)
    except Exception:
        return 0

def check_jules_first_comment(pr_number):
    """Rule 68: Detect Jules-created PRs by checking the first comment
    for known Jules signature phrases."""
    if not pr_number:
        return False
    try:
        cmd = ["gh", "pr", "view", pr_number, "--json", "comments", "--jq", ".comments[0].body"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        first_comment = result.stdout.strip()
        return ("PR created automatically by Jules" in first_comment or
                "Jules, reporting for duty" in first_comment)
    except Exception:
        return False

def add_jules_label(pr_number):
    """Rule 68: Add the 'jules' label to Jules-created PRs."""
    add_pr_labels(pr_number, ["jules"])

def sync_pr_labels_and_assignee(pr_number, pr_title, pr_body):
    """Sync labels from linked issues to the PR and assign the git user.

    This runs BEFORE any review work, ensuring every PR has correct
    labels and an assignee from the moment it's opened.

    1. Parse 'Closes #NNN' / 'Fixes #NNN' / 'Resolves #NNN' from PR body.
    2. Fetch labels from each linked issue.
    3. Add those labels to the PR (deduplicated by gh).
    4. Assign git user (from git config user.name) to the PR.
    5. Add 'bug' label if the PR title starts with 'fix('.
    """
    if not pr_number:
        return

    # Get git user for assignee
    try:
        git_user = subprocess.run(["git", "config", "user.name"], capture_output=True, text=True, check=True).stdout.strip()
    except Exception:
        git_user = "alsotoes"

    labels_to_add = set()

    # Parse issue references from PR body
    issue_pattern = re.compile(r'\b(?:Closes|Fixes|Resolves)\s+#(\d+)', re.IGNORECASE)
    issue_nums = issue_pattern.findall(pr_body or "")

    for issue_num in issue_nums:
        try:
            result = _gh_api("GET", f"repos/{_repo_slug()}/issues/{issue_num}")
            data = json.loads(result.stdout)
            for label in data.get("labels", []):
                labels_to_add.add(label["name"])
        except Exception as e:
            print(f"Failed to fetch labels for issue #{issue_num}: {e}", file=sys.stderr)

    # Add 'bug' label for fix() PRs even without issue links
    if pr_title and pr_title.lower().startswith("fix("):
        labels_to_add.add("bug")
    elif pr_title and pr_title.lower().startswith("feat("):
        labels_to_add.add("enhancement")

    # Apply labels
    if labels_to_add:
        add_pr_labels(pr_number, labels_to_add)

    # Assign git user to every PR
    assign_pr(pr_number, git_user)

def pr_has_label(pr_number, label):
    """Return True if the PR carries the given label (e.g. 'enhancement')."""
    try:
        cmd = ["gh", "pr", "view", pr_number, "--json", "labels"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        data = json.loads(result.stdout)
        return any(l.get("name") == label for l in data.get("labels", []))
    except Exception:
        return False


def get_current_pr_body(pr_number):
    """Rule 90: Fetch the CURRENT PR body from the GitHub API instead of relying
    on the (possibly stale) webhook event payload. The event payload reflects the
    body at event time; after create_missing_issue appends 'Resolves <url>' via
    gh pr edit, subsequent synchronize events may still carry the old body,
    causing duplicate auto-trace issues. Fetching live avoids that."""
    if not pr_number:
        return ""
    try:
        cmd = ["gh", "pr", "view", str(pr_number), "--json", "body", "--jq", ".body"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return result.stdout.strip()
    except Exception:
        return ""


def find_existing_auto_trace(pr_number, pr_title):
    """Rule 90: Search for an existing OPEN auto-trace issue already tracking this
    PR (title pattern '[Auto-Trace] <PR title>' or body mentioning 'for PR #<n>').
    Returns the canonical issue number, or None. This prevents the reviewer from
    creating a new auto-trace issue on every synchronize event when the PR body
    never gained a 'Resolves #N' link."""
    if not pr_title or not pr_number:
        return None
    expected_title = f"[Auto-Trace] {pr_title}"
    try:
        cmd = ["gh", "issue", "list", "--state", "open", "--limit", "50",
               "--search", f'"[Auto-Trace] {pr_title}" in:title',
               "--json", "number,title"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        issues = json.loads(result.stdout)
        for issue in issues:
            if issue.get("title") == expected_title:
                return issue["number"]
            # Fallback: body references this PR by number
        # Secondary scan: issues mentioning 'for PR #<n>' in body
        cmd = ["gh", "issue", "list", "--state", "open", "--limit", "50",
               "--search", f'for PR #{pr_number} in:body',
               "--json", "number,title"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        issues = json.loads(result.stdout)
        for issue in issues:
            if f"for PR #{pr_number}" in issue.get("title", ""):
                return issue["number"]
    except Exception as e:
        print(f"Failed to search for existing auto-trace issue: {e}", file=sys.stderr)
    return None


def has_openspec_change():
    """Rule 73: Return True if the PR branch adds any change under openspec/changes/."""
    try:
        cmd = ["git", "diff", "origin/master...HEAD", "--name-only", "--", "openspec/changes"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return any(line.strip() for line in result.stdout.splitlines())
    except Exception:
        return False


def has_blog_post():
    """Rule 76: Return True if the PR branch adds/updates a post under docs/blog/posts/."""
    try:
        cmd = ["git", "diff", "origin/master...HEAD", "--name-only", "--", "docs/blog/posts"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        return any(line.strip() for line in result.stdout.splitlines())
    except Exception:
        return False


def create_missing_issue(pr_number, pr_title, pr_body):
    # Rule 90: Deduplicate — never create a second auto-trace issue for a PR
    # that already has one. Reuse the canonical issue and ensure the PR body
    # carries 'Resolves #<canonical>' so future runs short-circuit.
    try:
        existing = find_existing_auto_trace(pr_number, pr_title)
        if existing is not None:
            print(f"Rule 90: Reusing existing auto-trace issue #{existing} for PR #{pr_number} (no duplicate created)")
            # Ensure the PR body links the canonical issue
            if f"Resolves #{existing}" not in pr_body and f"resolves #{existing}" not in pr_body.lower():
                set_pr_body(pr_number, f"{pr_body}\n\nResolves #{existing}")
            return True

        print(f"Rule 11 Violation detected. Autonomously creating tracking issue for PR #{pr_number}...")

        issue_title = f"[Auto-Trace] {pr_title}"
        issue_body = f"This issue was created autonomously to satisfy Rule 11 (Traceability) for PR #{pr_number}.\n\n### Original PR Description:\n{pr_body}"

        # Create the issue (assignee from git config)
        try:
            git_user = subprocess.run(["git", "config", "user.name"], capture_output=True, text=True, check=True).stdout.strip()
        except Exception:
            git_user = "alsotoes"

        # Issue #1140: Sentinel (security/bug) PRs get 'bug'; everything else
        # gets 'enhancement'. Always tag 'automation'.
        category = "bug" if "sentinel" in (pr_title or "").lower() else "enhancement"
        issue_number = create_issue_rest(issue_title, issue_body, [category, "automation"], git_user)
        if not issue_number:
            return False

        # Link the issue back to the PR using the 'Resolves' keyword
        set_pr_body(pr_number, f"{pr_body}\n\nResolves #{issue_number}")
        print(f"Successfully created and linked issue #{issue_number}")
        return True
    except Exception as e:
        print(f"Failed to create autonomous issue: {e}", file=sys.stderr)
        return False

def check_ci_and_merge(pr_number):
    try:
        # Use gh to get the status rollup of all checks
        cmd = ["gh", "pr", "view", pr_number, "--json", "statusCheckRollup"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        data = json.loads(result.stdout)
        
        rollup = data.get("statusCheckRollup", [])
        
        # 🛡️ Zero-Crash: Verify no checks have FAILED
        failures = [c for c in rollup if c.get("conclusion") == "FAILURE"]
        if failures:
            print(f"Merge aborted: {len(failures)} checks failed.")
            return

        # ⚡ Bolt: Execute automated merge. 
        # Using --auto handles PENDING checks by enabling GitHub's auto-merge feature.
        print(f"All steering rules respected. Enabling auto-merge for PR #{pr_number}...")
        subprocess.run(["gh", "pr", "merge", pr_number, "--merge", "--auto"], check=True)
        
    except Exception as e:
        print(f"Error during CI audit/merge: {e}", file=sys.stderr)

def main():
    api_key = os.environ.get("GEMINI_API_KEY")
    if not api_key:
        print("GEMINI_API_KEY not set", file=sys.stderr)
        sys.exit(1)
        
    model = os.environ.get("GEMINI_MODEL")
    if not model:
        model = "gemini-1.5-flash"
    
    pr_author = os.environ.get("PR_AUTHOR", "")
    pr_body = os.environ.get("PR_BODY", "")
    pr_title = os.environ.get("PR_TITLE", "")
    pr_number = os.environ.get("PR_NUMBER", "")

    # Rule 90: Use the CURRENT PR body from the API, not the (stale) webhook
    # event payload. The event payload reflects the body at event time; after a
    # previous run appended 'Resolves <url>' via gh pr edit, later synchronize
    # events can still carry the old body, causing duplicate auto-trace issues.
    current_body = get_current_pr_body(pr_number) if pr_number else ""
    if current_body:
        pr_body = current_body

    # ⚡ First: Sync labels from linked issues and assign alsotoes.
    # This ensures every PR has correct labels and an assignee before
    # any review work begins.
    sync_pr_labels_and_assignee(pr_number, pr_title, pr_body)

    # Rule 68: Detect Jules PRs by author, body, or first comment
    # Rule 68: Detect Jules PRs by author, the Jules-generated body marker, or
    # the first comment. Do NOT match a bare "jules" substring in the body — that
    # misfires on PRs that merely discuss Jules (issue #1140).
    is_jules_pr = (
        "jules" in pr_author.lower()
        or "PR created automatically by Jules" in pr_body
        or check_jules_first_comment(pr_number)
    )
    
    # 🛡️ Rule 11: Check for Issue-Spec Traceability
    # Detect both the full URL format (github.com/alsotoes/momo/issues/NNN)
    # and the GitHub shorthand format (Resolves #NNN, Closes #NNN, Fixes #NNN).
    # Without the shorthand check, the bot would create duplicate auto-trace
    # issues on every review cycle when PRs use the #NNN convention.
    has_issue_link = "github.com/alsotoes/momo/issues/" in pr_body or bool(re.search(r'\b(Resolves|Closes|Fixes)\s+#\d+', pr_body, re.IGNORECASE))
    traceability_instruction = ""
    if not has_issue_link and pr_number:
        # ⚡ Bolt: Autonomously resolve Rule 11 violation
        if create_missing_issue(pr_number, pr_title, pr_body):
            has_issue_link = True # Mark as resolved for the current run
        else:
            traceability_instruction = "\n- 🚨 VIOLATION (Rule 11): This PR is missing a link to a GitHub Issue. Remind the author that ALL PRs must be mirrored as issues for traceability."

    # Rule 73: Spec-First Implementation Mandate — feature/enhancement PRs MUST
    # carry an OpenSpec change in the same PR (openspec/changes/<id>/{proposal.md,
    # specs/<id>/spec.md linked to the GitHub issue, tasks.md}). Only enforced for
    # enhancement PRs; bug fixes are exempt but still need a tracking issue.
    spec_instruction = ""
    if pr_number and pr_has_label(pr_number, "enhancement") and not has_openspec_change():
        spec_instruction = "\n- 🚨 VIOLATION (Rule 73): This enhancement PR ships without an OpenSpec change under `openspec/changes/`. ALL features MUST include `proposal.md`, `specs/<id>/spec.md` (linked to the GitHub issue at the top), and `tasks.md` in the same PR, per Rule 73. Do not approve/merge until the change is added."

    # Rule 76: Blog Post Per Ratified Change — feature/enhancement PRs that ship a
    # ratifiable spec surface MUST also add a post under docs/blog/posts/ (or carry
    # an explicit no-blog justification in the PR description). Enforced for
    # enhancement PRs with an OpenSpec change; internal-only changes with a
    # no-blog justification in the PR body are exempt.
    blog_instruction = ""
    if (
        pr_number
        and pr_has_label(pr_number, "enhancement")
        and has_openspec_change()
        and not has_blog_post()
        and "no-blog" not in (pr_body or "").lower()
    ):
        blog_instruction = "\n- 🚨 VIOLATION (Rule 76): This enhancement PR ships an OpenSpec change without a matching blog post under `docs/blog/posts/`. Every ratified feature change MUST carry a Hugo-format post (same PR or immediately-following) with date=anchor issue/PR createdAt, implemented-state grounding, Bolt/Sentinel tags where relevant, plus artifacts/related front matter, per Rule 76. If the change is internal-only with no narrative value, add an explicit `no-blog` justification to the PR description to be exempt. Do not approve/merge until a post (or justification) is added."

    # ⚡ Bolt: Automated PR Management
    if pr_number:
        # Labeling for Jules PRs (Rule 68)
        if is_jules_pr:
            add_jules_label(pr_number)
            if "sentinel" in pr_title.lower():
                add_pr_labels(pr_number, ["bug"])
            elif "bolt" in pr_title.lower():
                add_pr_labels(pr_number, ["enhancement"])

    jules_commits = get_jules_commit_count()
    max_jules_pushes = 3

    diff = get_filtered_diff()
    if not diff:
        print("No relevant changes to review.")
        return

    rules_path = "openspec/config.yaml"
    rules = ""
    if os.path.exists(rules_path):
        try:
            import yaml
            with open(rules_path, "r") as f:
                config = yaml.safe_load(f)
                rules = config.get("context", "")
        except Exception as e:
            print(f"Error reading rules from YAML: {e}", file=sys.stderr)

    jules_instruction = ""
    if is_jules_pr:
        if jules_commits >= max_jules_pushes:
            jules_instruction = f"\n- 🛑 AI LOOP CIRCUIT BREAKER: @google-labs-jules has already made {jules_commits} attempts to fix issues. Do NOT tag him anymore. Instead, address your findings to the maintainer @alsotoes and state that manual intervention is required."
        else:
            jules_instruction = "\n- IMPORTANT: This PR was created by @google-labs-jules. Address your findings to him by tagging @google-labs-jules so he can fix them automatically."

    prompt = f"""You are an expert Go developer and security auditor.
Review the following Pull Request diff against the provided Project Steering Rules.

STEERING RULES:
{rules}

PR DIFF:
{diff}

TASK:
- Identify violations of the Zero-Crash Pattern (missing recover, unbounded readers).
- Ensure error mappings use syscall constants (POSIX Error Mapping).
- Look for performance bottlenecks (unnecessary allocations in hot paths).
- Check for security issues (path traversal, sanitization).{jules_instruction}{traceability_instruction}{spec_instruction}{blog_instruction}

INSTRUCTIONS:
- Be concise.
- If everything looks good, just say "✅ All Project Steering Rules and architectural patterns are respected."
- Do NOT repeat the diff or the rules.
- Format your response as a GitHub comment."""

    review = call_gemini(api_key, model, prompt)
    if review:
        is_approved = review_is_approved(review)

        # Issue #1134: append ECC Tools' advisory PR-audit verdicts. This is
        # appended AFTER is_approved is computed and never changes the decision.
        ecc_summary = get_ecc_audit_summary(pr_number)
        if ecc_summary:
            review = review.rstrip() + "\n\n---\n" + ecc_summary

        # Rule 69: For Jules PRs with actionable findings, post directly as
        # alsotoes (GITHUB_TOKEN = PAT) so Jules can recognize and act on it.
        # For ✅ reviews or non-Jules PRs, print to stdout and let the workflow
        # post as github-actions[bot] (BOT_TOKEN).
        if is_jules_pr and not is_approved and pr_number:
            try:
                subprocess.run(["gh", "pr", "comment", pr_number, "--body", review],
                               check=True)
                print("Review posted as alsotoes (Rule 69 — Jules PR)", file=sys.stderr)
            except Exception as e:
                print(f"Failed to post review as alsotoes: {e}", file=sys.stderr)
                print(review)
        else:
            print(review)

        # 🚀 Final Validation & Auto-Merge Gate
        if is_approved and pr_number:
            check_ci_and_merge(pr_number)

if __name__ == "__main__":
    main()
