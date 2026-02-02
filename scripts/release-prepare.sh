#!/bin/bash
# Best Practice 2026: Mechanized Release Preparation
# Automates the bump, rendering, and manifest update for a new release.

set -euo pipefail

# Fail-closed toolchain governance
export GOTOOLCHAIN=local

REPO_ROOT="$(git rev-parse --show-toplevel)"
NEW_VERSION="${1:-}"

if [[ -z "$NEW_VERSION" ]]; then
    echo "❌ Usage: $0 <VERSION> (e.g. 3.1.6)"
    exit 1
fi

# 0. Behavioral Changes Check (Governance Gate)
# Ensures that significant changes (like config defaults) are officially acknowledged.
if [[ ! -f "docs/releases/v${NEW_VERSION}_behavioral_changes.txt" ]]; then
    echo "⚠️  No behavioral changes file found: docs/releases/v${NEW_VERSION}_behavioral_changes.txt"
    echo "   If there are NO behavioral changes, create an empty file with that name."
    echo "   If there ARE changes (e.g. HLS.SegmentSeconds 4->6), document them there."
    exit 1
fi

echo "🚀 Preparing Release v${NEW_VERSION}..."

# 1. SemVer Validation (Strict)
if [[ ! "$NEW_VERSION" =~ ^[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
    echo "❌ Invalid SemVer format: ${NEW_VERSION}"
    exit 1
fi

# 2. Clean Working Tree Check
if [[ -n "$(git status --porcelain)" ]]; then
    echo "❌ Working tree is not clean. Commit or stash changes before preparation."
    exit 1
fi

# 3. Update VERSION
echo "$NEW_VERSION" > "${REPO_ROOT}/VERSION"
echo "✅ VERSION updated to ${NEW_VERSION}"

# 3b. Add placeholder to DIGESTS.lock to satisfy verification gates
# This will be replaced by release-verify-remote after publishing.
if ! grep -q "\"${NEW_VERSION}\":" "${REPO_ROOT}/DIGESTS.lock"; then
    cat <<EOF >> "${REPO_ROOT}/DIGESTS.lock"
  "${NEW_VERSION}":
    digest: "pending"
    published_at: "pending"
EOF
    echo "✅ Placeholder added to DIGESTS.lock"
fi

# 4. Render Documentation (Idempotent)
make docs-render

# 4b. Record Behavioral Changes to Walkthrough/Changelog
# This ensures they are part of the commit history.
echo "### Behavioral Changes (v${NEW_VERSION})" >> "${REPO_ROOT}/CHANGELOG.md"
cat "docs/releases/v${NEW_VERSION}_behavioral_changes.txt" >> "${REPO_ROOT}/CHANGELOG.md"
echo -e "\n" >> "${REPO_ROOT}/CHANGELOG.md"

# 5. Update RELEASE_MANIFEST.json
# Updated exclusively here per Hard Condition #1
GIT_SHA=$(git rev-parse HEAD)
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
IMAGE_REPO=$(grep "image:" "${REPO_ROOT}/DIGESTS.lock" | awk '{print $2}' | tr -d '[:space:]')

cat <<EOF > "${REPO_ROOT}/RELEASE_MANIFEST.json"
{
  "version": "${NEW_VERSION}",
  "git_sha": "${GIT_SHA}",
  "image": "${IMAGE_REPO}",
  "tag": "${NEW_VERSION}",
  "digest": null,
  "build_time_utc": "${BUILD_TIME}",
  "provenance_ref": null,
  "sbom_ref": null
}
EOF
echo "✅ RELEASE_MANIFEST.json updated (digest set to null for now)"

# 6. Final Verification (Local)
echo "🧪 Running final verification gates..."
make verify || (echo "❌ Verification failed. Fix drift or errors." && exit 1)

echo "✨ Release preparation complete for v${NEW_VERSION}."
echo "📝 Please review and commit: VERSION, RELEASE_MANIFEST.json, and generated docs."
