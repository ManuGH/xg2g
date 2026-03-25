#!/bin/bash
# Best Practice 2026: Zero-Drift Document Renderer
# Compiles templates into deploy bundle truth and compatibility mirrors using
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

render_deploy_unit_bundle() {
    local src="$1"
    local deploy_dst="$2"
    local compat_dst="$3"
    local body

    body="$(mktemp)"
    trap 'rm -f "${body}"' RETURN

    render_body "$src" "$body"

    {
        printf '%s\n' '# Canonical deploy bundle file for xg2g systemd installation.'
        cat "$body"
    } > "$deploy_dst"
    echo "✅ Rendered: ${deploy_dst}"

    {
        printf '%s\n' '# GENERATED FILE - DO NOT EDIT. Source: deploy/xg2g.service'
        tail -n +2 "$deploy_dst"
    } > "$compat_dst"
    echo "✅ Rendered: ${compat_dst}"
}

render_deploy_compose_bundle() {
    local src="$1"
    local deploy_dst="$2"
    local compat_dst="$3"
    local infra_dst="$4"
    local body

    body="$(mktemp)"
    trap 'rm -f "${body}"' RETURN

    render_body "$src" "$body"

    {
        printf '%s\n' '# Canonical deploy bundle file for xg2g production compose.'
        printf '%s\n' '# Step-1 migration note: keep content aligned with repo-root docker-compose.yml'
        printf '%s\n' '# until legacy verification and rendering paths are rewired.'
        cat "$body"
    } > "$deploy_dst"
    echo "✅ Rendered: ${deploy_dst}"

    {
        printf '%s\n' '# GENERATED FILE - DO NOT EDIT. Source: deploy/docker-compose.yml'
        tail -n +4 "$deploy_dst"
    } > "$compat_dst"
    echo "✅ Rendered: ${compat_dst}"

    {
        printf '%s\n' '# GENERATED FILE - DO NOT EDIT. Source: deploy/docker-compose.yml'
        tail -n +4 "$deploy_dst"
    } > "$infra_dst"
    echo "✅ Rendered: ${infra_dst}"
}

# 1. README.md
render "${BACKEND_ROOT}/templates/README.md.tmpl" "${REPO_ROOT}/README.md" "md"

# 2. systemd Unit bundle + compatibility mirror
render_deploy_unit_bundle \
    "${BACKEND_ROOT}/templates/docs/ops/xg2g.service.tmpl" \
    "${REPO_ROOT}/deploy/xg2g.service" \
    "${REPO_ROOT}/docs/ops/xg2g.service"

# 3. docker-compose bundle + compatibility mirrors
render_deploy_compose_bundle \
    "${BACKEND_ROOT}/templates/docker-compose.yml.tmpl" \
    "${REPO_ROOT}/deploy/docker-compose.yml" \
    "${REPO_ROOT}/docker-compose.yml" \
    "${REPO_ROOT}/infrastructure/docker/docker-compose.yml"

# 4. Deployment Runtime Contract
render "${BACKEND_ROOT}/templates/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md.tmpl" "${REPO_ROOT}/docs/ops/DEPLOYMENT_RUNTIME_CONTRACT.md" "md"

# 5. Operations Model
render "${BACKEND_ROOT}/templates/docs/ops/OPERATIONS_MODEL.md.tmpl" "${REPO_ROOT}/docs/ops/OPERATIONS_MODEL.md" "md"

# 6. Continuous Verifier Units
render "${BACKEND_ROOT}/templates/docs/ops/xg2g-verifier.service.tmpl" "${REPO_ROOT}/docs/ops/xg2g-verifier.service" "shell"
render "${BACKEND_ROOT}/templates/docs/ops/xg2g-verifier.timer.tmpl" "${REPO_ROOT}/docs/ops/xg2g-verifier.timer" "shell"

echo "✨ Documentation rendering complete (idempotent)."
