#!/bin/bash
# Best Practice 2026: Zero-Drift Document Renderer
# Compiles templates into production artifacts using SSoT (VERSION, DIGESTS.lock).

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
BACKEND_ROOT="${REPO_ROOT}/backend"

# VERSION moved from repo root to backend/ in the monorepo layout.
VERSION_FILE="${REPO_ROOT}/VERSION"
if [[ ! -f "${VERSION_FILE}" ]]; then
    VERSION_FILE="${BACKEND_ROOT}/VERSION"
fi
VERSION="$(cat "${VERSION_FILE}" | tr -d '[:space:]')"

echo "🛠️  Rendering docs for version: ${VERSION}"

# Extract Digest from DIGESTS.lock if present
# This is a basic parser for Step 1; Step 2 will rely on more robust validation.
DIGEST_VAL=""
if [[ -f "${REPO_ROOT}/DIGESTS.lock" ]]; then
    # Look for the digest line after the version key
    DIGEST_VAL=$(grep -A 1 "\"${VERSION}\":" "${REPO_ROOT}/DIGESTS.lock" | grep "digest:" | awk '{print $2}' | tr -d '"' | tr -d '[:space:]') || true
fi

render() {
    local src="$1"
    local dst="$2"
    local mode="$3" # "md" or "shell"

    local rel_src="${src#$REPO_ROOT/}"

    # Standard Header
    local header=""
    if [[ "$mode" == "md" ]]; then
        header="<!-- GENERATED FILE - DO NOT EDIT. Source: ${rel_src} -->\n"
    else
        header="# GENERATED FILE - DO NOT EDIT. Source: ${rel_src}\n"
    fi

    printf "$header" > "$dst"

    # Replace placeholders and append
    sed -e "s/{{VERSION}}/${VERSION}/g" \
        -e "s/{{DIGEST}}/${DIGEST_VAL}/g" \
        "$src" >> "$dst"

    echo "✅ Rendered: ${dst}"
}

# 1. README.md
render "${BACKEND_ROOT}/templates/README.md.tmpl" "${REPO_ROOT}/README.md" "md"

# 2. systemd Unit
render "${BACKEND_ROOT}/templates/docs/ops/xg2g.service.tmpl" "${REPO_ROOT}/docs/ops/xg2g.service" "shell"

# 3. docker-compose.yml
render "${BACKEND_ROOT}/templates/docker-compose.yml.tmpl" "${REPO_ROOT}/docker-compose.yml" "shell"

# 4. Deployment Runtime Contract
render "${BACKEND_ROOT}/templates/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md.tmpl" "${REPO_ROOT}/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md" "md"

# 5. Operations Model
render "${BACKEND_ROOT}/templates/docs/ops/OPERATIONS_MODEL.md.tmpl" "${REPO_ROOT}/docs/ops/OPERATIONS_MODEL.md" "md"

# 6. Continuous Verifier Units
render "${BACKEND_ROOT}/templates/docs/ops/xg2g-verifier.service.tmpl" "${REPO_ROOT}/docs/ops/xg2g-verifier.service" "shell"
render "${BACKEND_ROOT}/templates/docs/ops/xg2g-verifier.timer.tmpl" "${REPO_ROOT}/docs/ops/xg2g-verifier.timer" "shell"

echo "✨ Documentation rendering complete (idempotent)."
