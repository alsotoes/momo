#!/bin/bash
# Developer helper for local/manual troubleshooting only.
# Automated scans are executed in CI via .github/workflows/go.yml.
#
# Exit on error
set -e

# Exit on pipe fails (if possible)
( set -o pipefail 2> /dev/null ) || true

# Always execute from the repository root
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$REPO_ROOT"

RUN_TESTS=false
DEBUG_MODE=false
WAIT_FOR_QG=true

for arg in "$@"; do
    case "$arg" in
        --with-tests|-t)
            RUN_TESTS=true
            ;;
        --debug|-d)
            DEBUG_MODE=true
            ;;
        --no-wait|-n)
            WAIT_FOR_QG=false
            ;;
        --help|-h)
            echo "Usage: $0 [OPTIONS]"
            echo "Options:"
            echo "  -t, --with-tests   Run all unit tests to generate fresh coverage reports (coverage.out)"
            echo "  -d, --debug        Run SonarCloud scanner in debug mode (-X)"
            echo "  -n, --no-wait      Do not block/fail on Quality Gate calculation"
            echo "  -h, --help         Show this help message"
            exit 0
            ;;
        *)
            echo "Unknown option: $arg" >&2
            echo "Use --help for usage information." >&2
            exit 1
            ;;
    esac
done

if [[ "$RUN_TESTS" == true ]]; then
    echo "=== Running Go Tests & Generating Coverage (coverage.out) ==="
    make coverage
elif [[ ! -f "coverage.out" ]]; then
    echo "INFO: coverage.out not found. Run with -t / --with-tests to generate coverage reports."
fi

# Automatically load token from ~/.bashrc if not already in environment
if [[ -f "$HOME/.bashrc" && -z "$SONAR_TOKEN" && -z "$SONARQUBE_TOKEN_HEADUP" ]]; then
    eval "$(grep -E '^[[:space:]]*export[[:space:]]+(SONARQUBE_TOKEN_HEADUP|SONAR_TOKEN)=' "$HOME/.bashrc" 2>/dev/null || true)"
fi

mkdir -p "$HOME/.sonar"
export SONAR_TOKEN="${SONAR_TOKEN:-${SONARQUBE_TOKEN_HEADUP:-}}"
export SONAR_SCANNER_VERSION="8.1.0.6389"
export SONAR_HOST_URL="https://sonarcloud.io"

if [[ -z "$SONAR_TOKEN" ]]; then
    echo "ERROR: Sonar token is not set. Please export SONAR_TOKEN or SONARQUBE_TOKEN_HEADUP." >&2
    exit 1
fi

# Cache scanner download locally
if [[ ! -d "$HOME/.sonar/sonar-scanner-${SONAR_SCANNER_VERSION}-linux-x64" ]]; then
    echo "=== Downloading SonarCloud CLI ==="
    curl --proto '=https' --tlsv1.2 -sSLo "$HOME/.sonar/sonar-scanner.zip" "https://binaries.sonarsource.com/Distribution/sonar-scanner-cli/sonar-scanner-cli-${SONAR_SCANNER_VERSION}-linux-x64.zip"
    unzip -o "$HOME/.sonar/sonar-scanner.zip" -d "$HOME/.sonar/"
fi

export PATH="$PATH:$HOME/.sonar/sonar-scanner-${SONAR_SCANNER_VERSION}-linux-x64/bin"

echo "=== Running SonarCloud Scanner ==="
SCANNER_FLAGS=("-Dproject.settings=./sonar-project.properties")

if [[ "$DEBUG_MODE" == true ]]; then
    SCANNER_FLAGS+=("-X")
fi

if [[ "$WAIT_FOR_QG" == true ]]; then
    SCANNER_FLAGS+=("-Dsonar.qualitygate.wait=true")
else
    SCANNER_FLAGS+=("-Dsonar.qualitygate.wait=false")
fi

sonar-scanner "${SCANNER_FLAGS[@]}"
