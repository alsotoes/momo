#!/bin/bash
set -euo pipefail

# ============================================================
# Rule 29 Enforcement: Scanner-Safe Test Secrets
# Scans all source files for dummy auth tokens and verifies
# they have a trailing `notsecret` annotation.
# ============================================================

REPO_ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
cd "$REPO_ROOT"

VIOLATIONS=0

# Patterns that indicate a dummy token/secret
PATTERNS=(
    'a1b2c3d4e5f6g7h8i9j0k1l2m3n4o5p6q7r8s9t0u1v2w3x4y5z6a1b2c3d4e5f6'
    'super_secret_token'
    'k8s-momo-token'
    'test-token'
    'AKIAIOSFODNN7EXAMPLE'
    'wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY'
)

# File extensions to scan
EXTENSIONS=('go' 'sh' 'yml' 'yaml' 'js' 'conf' 'json' 'py')

for pattern in "${PATTERNS[@]}"; do
    for ext in "${EXTENSIONS[@]}"; do
        while IFS= read -r line; do
            # Skip vendor directory
            [[ "$line" == vendor/* ]] && continue
            # Skip if line already has notsecret annotation
            if echo "$line" | rg -q 'notsecret'; then
                continue
            fi
            echo "VIOLATION (Rule 29): $line"
            VIOLATIONS=$((VIOLATIONS + 1))
        done < <(rg -n "$pattern" -g "*.$ext" -g '!vendor/' --no-heading 2>/dev/null || true)
    done
done

if [ "$VIOLATIONS" -gt 0 ]; then
    echo ""
    echo "=== FAIL: $VIOLATIONS violation(s) of Rule 29 (Scanner-Safe Test Secrets) ==="
    echo "All dummy tokens must have a trailing '// notsecret' or '# notsecret' annotation."
    exit 1
fi

echo "=== PASS: All dummy tokens are annotated with notsecret ==="
