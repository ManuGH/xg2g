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
    ".node-version"
    ".nvmrc"
    ".pre-commit-config.yaml"
    ".secrets.baseline"
    ".trivyignore"
    ".vscode/"

    # Top-level documentation
    "AGENTS.md"
    "ARCHITECTURE.md"
    "CHANGELOG.md"
    "DIGESTS.lock"
    "LICENSE"
    "README.md"
    "TECH_DEBT.md"

    # Build & container
    "Dockerfile"
    "Makefile"
    "mk/"
    "cliff.toml"
    "mise.toml"
    "RELEASE_MANIFEST.json"

    # Source directories
    "android/"
    "apps/"
    "backend/"
    "hack/"
    "scripts/"

    # Test fixtures (canonical location enforced by ci/check-test-assets-location.sh)
    "testdata/"

    # Infrastructure & deployment
    "design/"
    "docs/"
    "infra/"
    "openapi/"
    "support/"

    # Docker Compose
    "compose.dev.yaml"
    "compose.monitoring.yaml"

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

    # Ignored local runtime/build outputs are allowed to exist in a developer
    # workspace. Root purity only guards non-ignored source-tree entries.
    if git check-ignore --no-index -q -- "$item"; then
        continue
    fi

    if [[ ! "$item" =~ $ALLOWLIST_REGEX ]]; then
        echo "❌ VIOLATION: Forbidden item in root: $item"
        VIOLATIONS=$((VIOLATIONS + 1))
    fi
done
shopt -u dotglob

if [ "$VIOLATIONS" -gt 0 ]; then
    echo "--------------------------------------------------------"
    echo "🚨 FAIL: Root purity check failed with $VIOLATIONS violations."
    echo "💡 Root must remain clean. Move local outputs to ignored paths such as 'artifacts/' or 'tmp/'."
    echo "--------------------------------------------------------"
    exit 1
fi

echo "✅ PASS: Repository Root is Pure."
exit 0
