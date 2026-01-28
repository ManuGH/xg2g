#!/bin/bash
# scripts/ci_gate_root_purity.sh
# Purpose: Enforces "Clean by Construction" for repository root.
# Fails if any untracked or non-allowlisted file is found in reporoot.

set -e

# --- Configuration: Strict Allowlist ---
# Only these files and directories are allowed in repo root.
# Directories MUST end with /
ALLOWLIST=(
    ".claude/"
    ".dev-notes/"
    ".dev-setup"
    ".dockerignore"
    ".editorconfig"
    ".env"
    ".env.example"
    ".git/"
    ".gitattributes"
    ".githooks/"
    ".github/"
    ".gitignore"
    ".gitleaks.toml"
    ".golangci.yml"
    ".goreleaser.yml"
    ".markdownlint.json"
    ".pre-commit-config.yaml"
    ".secrets.baseline"
    "ARCHITECTURE.md"
    "BUILD.md"
    "CHANGELOG_V3.md"
    "Dockerfile"
    "Dockerfile.distroless"
    "LICENSE"
    "Makefile"
    "README.md"
    "api/"
    "artifacts/"
    "bin/"
    "build.sh"
    "cmd/"
    "config.example.yaml"
    "config.generated.example.yaml"
    "config.yaml"
    "contracts/"
    "cosign.bundle"
    "cspell.json"
    "data/"
    "docker-compose.dev.yml"
    "docker-compose.monitoring.yml"
    "docker-compose.yml"
    "docs/"
    "design/"
    "e2e/"
    "fixtures/"
    "go.mod"
    "go.sum"
    "grafana-dashboard.json"
    "internal/"
    "monitoring/"
    "openapi/"
    "package-lock.json"
    "package.json"
    "panic_allowlist.txt"
    "red/"
    "renovate.json"
    "run_dev.sh"
    "scripts/"
    "support/"
    "templates/"
    "test/"
    "testdata/"
    "tmp/"
    "tools/"
    "tools.go"
    "vendor/"
    "VERSION"
    "webui/"
)

echo "🔍 Verifying Repository Root Purity..."

# Convert allowlist array to a regex pattern
ALLOWLIST_REGEX="^($(IFS='|' ; echo "${ALLOWLIST[*]}"))$"
# Strip trailing slashes for regex matching on directory names
ALLOWLIST_REGEX=$(echo "$ALLOWLIST_REGEX" | sed 's|/||g')

VIOLATIONS=0

# Scan all files and directories in root (depth 1)
# Exclude current and parent dir
shopt -s dotglob
for item in *; do
    # Skip . and ..
    [[ "$item" == "." || "$item" == ".." ]] && continue
    
    if [[ ! "$item" =~ $ALLOWLIST_REGEX ]]; then
        echo "❌ VIOLATION: Forbidden item in root: $item"
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
done
shopt -u dotglob

if [ "$VIOLATIONS" -gt 0 ]; then
    echo "--------------------------------------------------------"
    echo "🚨 FAIL: Root purity check failed with $VIOLATIONS violations."
    echo "💡 Root must remain clean. Move results to 'artifacts/' or 'tmp/'."
    echo "--------------------------------------------------------"
    exit 1
fi

echo "✅ PASS: Repository Root is Pure."
exit 0
