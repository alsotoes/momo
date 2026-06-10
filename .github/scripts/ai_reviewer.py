import os
import subprocess
import json
import http.client
import sys

def get_filtered_diff():
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
            return "\n".join(lines[:max_diff_lines]) + "\n\n[DIFF TRUNCATED FOR TOKEN EFFICIENCY]"
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

def main():
    api_key = os.environ.get("GEMINI_API_KEY")
    if not api_key:
        print("GEMINI_API_KEY not set", file=sys.stderr)
        sys.exit(1)
        
    model = os.environ.get("GEMINI_MODEL")
    if not model:
        model = "gemini-1.5-flash"
    
    diff = get_filtered_diff()
    if not diff:
        print("No relevant changes to review.")
        return

    rules_path = "openspec/project.md"
    rules = ""
    if os.path.exists(rules_path):
        with open(rules_path, "r") as f:
            rules = f.read()

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
- Check for security issues (path traversal, sanitization).

INSTRUCTIONS:
- Be concise.
- If everything looks good, just say "✅ All Project Steering Rules and architectural patterns are respected."
- Do NOT repeat the diff or the rules.
- Format your response as a GitHub comment."""

    review = call_gemini(api_key, model, prompt)
    if review:
        print(review)

if __name__ == "__main__":
    main()
