#!/usr/bin/env bash
# ==============================================================================
# xg2g Go Toolchain Policy Guard
# ==============================================================================
# Ensures that:
# 1. No GOTOOLCHAIN_DISABLED_AUTO is present in CI, Makefile, or Dockerfiles.
# 2. Go version matches exactly what is defined in go.mod.
# 3. Dockerfile base images match the required Go version.
# ==============================================================================
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BACKEND_ROOT="${REPO_ROOT}/backend"
GO_MOD_RELATIVE_PATH="${BACKEND_ROOT#${REPO_ROOT}/}/go.mod"
FAIL=0

# 1. Source of Truth: go.mod
if [[ ! -f "${BACKEND_ROOT}/go.mod" ]]; then
    echo "❌ ERROR: go.mod not found at ${BACKEND_ROOT}" >&2
    exit 1
fi
EXPECTED_GO_VERSION=$(awk '/^go /{print $2}' "${BACKEND_ROOT}/go.mod")
echo "🔍 Source of Truth (go.mod): Go $EXPECTED_GO_VERSION"

# 2. Anti-Auto Check (Grep for GOTOOLCHAIN_DISABLED_AUTO)
echo "🔍 Checking for disallowed GOTOOLCHAIN_DISABLED_AUTO..."
# Exclude the script itself and vendor/ if it exists
AUTO_MATCHES=$(grep -rnH "GOTOOLCHAIN_DISABLED_AUTO" "${REPO_ROOT}" \
    --exclude-dir=.git \
    --exclude-dir=vendor \
    --exclude="$(basename "$0")" \
    --exclude="*.md" || true)

if [[ -n "$AUTO_MATCHES" ]]; then
    echo "❌ ERROR: GOTOOLCHAIN_DISABLED_AUTO found in the following locations (must be 'local'):" >&2
    echo "$AUTO_MATCHES" | sed 's/^/  /' >&2
    FAIL=1
fi

# 3. Go Version Match Check
# Check current environment Go version
CURRENT_GO_VERSION=$(go version | awk '{print $3}' | sed 's/go//')
# Match only major.minor for initial comparison, or exact if possible.
# go.mod usually has major.minor (e.g., 1.25), but go version might be 1.25.5
if [[ "$CURRENT_GO_VERSION" != "$EXPECTED_GO_VERSION"* ]]; then
    echo "❌ ERROR: Current Go version ($CURRENT_GO_VERSION) does not satisfy go.mod ($EXPECTED_GO_VERSION)" >&2
    FAIL=1
else
    echo "✅ Go environment matches go.mod ($CURRENT_GO_VERSION)"
fi

# 4. Dockerfile Version Verification
check_dockerfile() {
    local file="$1"
    local expected="$2"
    if [[ ! -f "$file" ]]; then return; fi
    
    echo "🔍 Checking $file..."
    local tags
    tags=$(grep -E '^FROM[[:space:]]+golang:' "$file" | awk '{print $2}' || true)
    
    # If no tags found, might be using digest - we'll need to handle that if P0.5
    if [[ -z "$tags" ]]; then
        # Check if it uses a digest @sha256
        if grep -qE '^FROM[[:space:]]+golang@sha256:' "$file"; then
            echo "  ℹ️  $file uses golang digest; manual verification required (P0.5)"
            return
        fi
        echo "❌ ERROR: No golang base image found in $file" >&2
        FAIL=1
        return
    fi

    while read -r tag; do
        [[ -z "$tag" ]] && continue
        local version="${tag#golang:}"
        version="${version%%-*}" # Remove -alpine, -bullseye etc
        if [[ "$version" != "$expected"* ]]; then
            echo "❌ ERROR: $file: golang base version '$version' does not match go.mod '$expected'" >&2
            FAIL=1
        fi
    done <<< "$tags"
}

check_dockerfile "${REPO_ROOT}/Dockerfile" "$EXPECTED_GO_VERSION"
check_dockerfile "${REPO_ROOT}/Dockerfile.distroless" "$EXPECTED_GO_VERSION"

# 5. Workflow Version Verification
echo "🔍 Checking CI workflows..."
if [[ -d "${REPO_ROOT}/.github/workflows" ]]; then
while IFS= read -r -d '' wf; do
    # Check go-version (if hardcoded)
    VERSIONS=$(grep -E '^[[:space:]]*go-version:[[:space:]]*' "$wf" | \
      sed -E 's/^[[:space:]]*go-version:[[:space:]]*"?([^" ]+)"?.*/\1/' || true)
    
    if [[ -n "$VERSIONS" ]]; then
        while read -r v; do
            [[ -z "$v" ]] && continue
            # Handle cases where version might be ${{ ... }} or similar (skip for now)
            if [[ "$v" == \$* ]]; then continue; fi
            if [[ "$v" != "$EXPECTED_GO_VERSION"* ]]; then
                echo "❌ ERROR: $wf: go-version '$v' does not match go.mod '$EXPECTED_GO_VERSION'" >&2
                FAIL=1
            fi
        done <<< "$VERSIONS"
    fi

    # Check go-version-file (must point at the repo's canonical backend go.mod)
    GO_MOD_FILES=$(grep -E '^[[:space:]]*go-version-file:[[:space:]]*' "$wf" | \
      sed -E 's/^[[:space:]]*go-version-file:[[:space:]]*"?([^" ]+)"?.*/\1/' || true)
    if [[ -n "$GO_MOD_FILES" ]]; then
        while read -r f; do
            [[ -z "$f" ]] && continue
            if [[ "$f" != "$GO_MOD_RELATIVE_PATH" ]]; then
                echo "❌ ERROR: $wf: go-version-file must be '$GO_MOD_RELATIVE_PATH', found '$f'" >&2
                FAIL=1
            fi
        done <<< "$GO_MOD_FILES"
    fi
done < <(find "${REPO_ROOT}/.github/workflows" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
fi

if [[ "$FAIL" -ne 0 ]]; then
    echo "❌ Toolchain policy verification FAILED." >&2
    exit 1
fi

echo "✅ Toolchain policy verification PASSED."
