#!/bin/bash
# Best Practice 2026: Zero-Drift Document Renderer
# Compiles templates into deploy bundle truth and generated docs using
# SSoT inputs (VERSION, DIGESTS.lock).

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

render_body() {
    local src="$1"
    local dst="$2"

    sed -e "s/{{VERSION}}/${VERSION}/g" \
        -e "s/{{DIGEST}}/${DIGEST_VAL}/g" \
        "$src" > "$dst"
}

render_deploy_unit() {
    local src="$1"
    local deploy_dst="$2"
    local body

    body="$(mktemp)"
    render_body "$src" "$body"

    {
        printf '%s\n' '# Canonical deploy bundle file for xg2g systemd installation.'
        cat "$body"
    } > "$deploy_dst"
    echo "✅ Rendered: ${deploy_dst}"
    rm -f "$body"
}

render_deploy_compose() {
    local src="$1"
    local deploy_dst="$2"
    local body

    body="$(mktemp)"

    render_body "$src" "$body"

    {
        printf '%s\n' '# Canonical deploy bundle file for xg2g production compose.'
        cat "$body"
    } > "$deploy_dst"
    echo "✅ Rendered: ${deploy_dst}"
    rm -f "$body"
}

# 1. README.md
render "${BACKEND_ROOT}/templates/README.md.tmpl" "${REPO_ROOT}/README.md" "md"

# 2. systemd Unit bundle
render_deploy_unit \
    "${BACKEND_ROOT}/templates/docs/ops/xg2g.service.tmpl" \
    "${REPO_ROOT}/deploy/xg2g.service"

# 3. docker-compose bundle
render_deploy_compose \
    "${BACKEND_ROOT}/templates/docker-compose.yml.tmpl" \
    "${REPO_ROOT}/deploy/docker-compose.yml"

# 4. Deployment Runtime Contract
render "${BACKEND_ROOT}/templates/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md.tmpl" "${REPO_ROOT}/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md" "md"

# 5. Operations Model
render "${BACKEND_ROOT}/templates/docs/ops/OPERATIONS_MODEL.md.tmpl" "${REPO_ROOT}/docs/ops/OPERATIONS_MODEL.md" "md"

# 6. Continuous Verifier Units
render "${BACKEND_ROOT}/templates/docs/ops/xg2g-verifier.service.tmpl" "${REPO_ROOT}/docs/ops/xg2g-verifier.service" "shell"
render "${BACKEND_ROOT}/templates/docs/ops/xg2g-verifier.timer.tmpl" "${REPO_ROOT}/docs/ops/xg2g-verifier.timer" "shell"

echo "✨ Documentation rendering complete (idempotent)."
