#!/bin/bash
# scripts/ci_gate_root_purity.sh
# Purpose: Enforces "Clean by Construction" for repository root.
# Fails if any untracked or non-allowlisted file is found in reporoot.

set -e

# --- Configuration: Strict Allowlist ---
# Only these files and directories are allowed in repo root.
# Directories MUST end with /
ALLOWLIST=(
    # Dotfiles & config
    ".claude/"
    ".dev/"
    ".dev-notes/"
    ".dev-setup"
    ".devcontainer/"
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
    ".goreleaser.yml"
    ".markdownlint.json"
    ".pre-commit-config.yaml"
    ".secrets.baseline"
    ".trivyignore"

    # Top-level documentation
    "AGENTS.md"
    "ARCHITECTURE.md"
    "CHANGELOG.md"
    "CODE_OF_CONDUCT.md"
    "CONTRIBUTING.md"
    "LICENSE"
    "README.md"
    "SECURITY.md"
    "TECHNICAL_DEBT_2026.md"

    # Build & container
    "Dockerfile"
    "Dockerfile.distroless"
    "Dockerfile.ffmpeg-base"
    "DIGESTS.lock"
    "Makefile"
    "mk/"
    "build.sh"
    "cliff.toml"
    "mise.toml"
    "RELEASE_MANIFEST.json"

    # Source directories
    "android/"
    "backend/"
    "frontend/"
    "hack/"

    # Infrastructure & deployment
    "deploy/"
    "design/"
    "docs/"
    "infrastructure/"
    "monitoring/"
    "openapi/"
    "support/"

    # Docker Compose
    "docker-compose.dev.yml"
    "docker-compose.monitoring.yml"

    # Go workspace
    "go.work"
    "go.work.sum"

    # Node / frontend tooling
    "cspell.json"
    "package-lock.json"
    "package.json"

    # Dev scripts
    "run_dev.sh"
    "run_ui_dev.sh"
    "run_android_local.sh"
    "run_android_tv_smoke.sh"

    # Testing
    "red/"
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
