import os
import subprocess
import json
import http.client
import sys

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

def create_missing_issue(pr_number, pr_title, pr_body):
    try:
        print(f"Rule 11 Violation detected. Autonomously creating tracking issue for PR #{pr_number}...")
        
        issue_title = f"[Auto-Trace] {pr_title}"
        issue_body = f"This issue was created autonomously to satisfy Rule 11 (Traceability) for PR #{pr_number}.\n\n### Original PR Description:\n{pr_body}"
        
        # Create the issue
        cmd = ["gh", "issue", "create", "--title", issue_title, "--body", issue_body, "--label", "enhancement"]
        result = subprocess.run(cmd, capture_output=True, text=True, check=True)
        issue_url = result.stdout.strip()
        
        # Link the issue back to the PR using the 'Resolves' keyword
        subprocess.run(["gh", "pr", "edit", pr_number, "--body", f"{pr_body}\n\nResolves {issue_url}"], check=True)
        print(f"Successfully created and linked issue: {issue_url}")
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
    is_jules_pr = "jules" in pr_author.lower() or "jules" in pr_body.lower()
    
    # 🛡️ Rule 11: Check for Issue-Spec Traceability
    has_issue_link = "github.com/alsotoes/momo/issues/" in pr_body
    traceability_instruction = ""
    if not has_issue_link and pr_number:
        # ⚡ Bolt: Autonomously resolve Rule 11 violation
        if create_missing_issue(pr_number, pr_title, pr_body):
            has_issue_link = True # Mark as resolved for the current run
        else:
            traceability_instruction = "\n- 🚨 VIOLATION (Rule 11): This PR is missing a link to a GitHub Issue. Remind the author that ALL PRs must be mirrored as issues for traceability."

    # ⚡ Bolt: Automated PR Management
    if pr_number:
        # 1. Labeling for Jules PRs
        if is_jules_pr:
            if "sentinel" in pr_title.lower():
                subprocess.run(["gh", "pr", "edit", pr_number, "--add-label", "bug"])
            elif "bolt" in pr_title.lower():
                subprocess.run(["gh", "pr", "edit", pr_number, "--add-label", "enhancement"])
        
        # 2. Assignment for alsotoes PRs
        if pr_author.lower() == "alsotoes":
            subprocess.run(["gh", "pr", "edit", pr_number, "--add-assignee", "alsotoes"])

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
- Check for security issues (path traversal, sanitization).{jules_instruction}{traceability_instruction}

INSTRUCTIONS:
- Be concise.
- If everything looks good, just say "✅ All Project Steering Rules and architectural patterns are respected."
- Do NOT repeat the diff or the rules.
- Format your response as a GitHub comment."""

    review = call_gemini(api_key, model, prompt)
    if review:
        print(review)
        
        # 🚀 Final Validation & Auto-Merge Gate
        if review.strip().startswith("✅") and pr_number:
            check_ci_and_merge(pr_number)

if __name__ == "__main__":
    main()
