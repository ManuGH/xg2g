#!/bin/bash
# Best Practice 2026: Mechanized Release Preparation
# Automates the bump, rendering, and manifest update for a new release.

set -euo pipefail

# Match CI semantics by default while still allowing explicit overrides.
export GOTOOLCHAIN="${GOTOOLCHAIN:-auto}"
export GOCACHE="${GOCACHE:-${TMPDIR:-/tmp}/xg2g-gocache}"
mkdir -p "${GOCACHE}"

REPO_ROOT="$(git rev-parse --show-toplevel)"
BACKEND_VERSION_FILE="${REPO_ROOT}/backend/VERSION"
VERSION_FALLBACK_FILE="${REPO_ROOT}/backend/internal/version/version.go"
NEW_VERSION_RAW="${1:-}"

if [[ -z "$NEW_VERSION_RAW" ]]; then
    echo "❌ Usage: $0 <VERSION> (e.g. v3.1.6)"
    exit 1
fi

PLAIN_VERSION="${NEW_VERSION_RAW#v}"
TAG_VERSION="v${PLAIN_VERSION}"

# 0. Behavioral Changes Check (Governance Gate)
# Ensures that significant changes (like config defaults) are officially acknowledged.
if [[ ! -f "docs/release/${TAG_VERSION}_behavioral_changes.txt" ]]; then
    echo "⚠️  No behavioral changes file found: docs/release/${TAG_VERSION}_behavioral_changes.txt"
    echo "   If there are NO behavioral changes, create an empty file with that name."
    echo "   If there ARE changes (e.g. HLS.SegmentSeconds 4->6), document them there."
    exit 1
fi

echo "🚀 Preparing Release ${TAG_VERSION}..."

# 1. SemVer Validation (Strict)
if [[ ! "$TAG_VERSION" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[a-zA-Z0-9.]+)?$ ]]; then
    echo "❌ Invalid SemVer format: ${TAG_VERSION}"
    exit 1
fi

# 2. Clean Working Tree Check
if [[ -n "$(git status --porcelain)" ]]; then
    echo "❌ Working tree is not clean. Commit or stash changes before preparation."
    exit 1
fi

# 3. Update backend/VERSION
echo "$TAG_VERSION" > "${BACKEND_VERSION_FILE}"
echo "✅ backend/VERSION updated to ${TAG_VERSION}"

# Keep the fallback version metadata aligned with the release tag.
python3 - <<EOF
from pathlib import Path
import re

path = Path("${VERSION_FALLBACK_FILE}")
new_version = "${TAG_VERSION}"
text = path.read_text()
updated = re.sub(
    r'Version = "v[^"]+"',
    f'Version = "{new_version}"',
    text,
    count=1,
)
if updated == text:
    raise SystemExit("failed to update backend/internal/version/version.go")
path.write_text(updated)
EOF
echo "✅ backend/internal/version/version.go updated to ${TAG_VERSION}"

# 3b. Add placeholder to DIGESTS.lock to satisfy verification gates.
# DIGESTS.lock is JSON; update it structurally so retries stay deterministic.
python3 - <<EOF
import json
from pathlib import Path

path = Path("${REPO_ROOT}/DIGESTS.lock")
data = json.loads(path.read_text())
data.setdefault("releases", {})
data["releases"]["${TAG_VERSION}"] = {
    "digest": "pending",
    "published_at": "pending",
}
path.write_text(json.dumps(data, indent=2) + "\n")
EOF
echo "✅ DIGESTS.lock placeholder synchronized for ${TAG_VERSION}"

# 4. Render Documentation (Idempotent)
make docs-render

# 4b. Record Behavioral Changes to Walkthrough/Changelog
# This ensures they are part of the commit history.
echo "### Behavioral Changes (${TAG_VERSION})" >> "${REPO_ROOT}/CHANGELOG.md"
cat "docs/release/${TAG_VERSION}_behavioral_changes.txt" >> "${REPO_ROOT}/CHANGELOG.md"
echo -e "\n" >> "${REPO_ROOT}/CHANGELOG.md"

# 5. Update RELEASE_MANIFEST.json
# Updated exclusively here per Hard Condition #1
GIT_SHA=$(git rev-parse HEAD)
BUILD_TIME=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
IMAGE_REPO=$(python3 - <<EOF
import json
from pathlib import Path

data = json.loads(Path("${REPO_ROOT}/DIGESTS.lock").read_text())
print(data["image"])
EOF
)

cat <<EOF > "${REPO_ROOT}/RELEASE_MANIFEST.json"
{
  "version": "${TAG_VERSION}",
  "git_sha": "${GIT_SHA}",
  "image": "${IMAGE_REPO}",
  "tag": "${TAG_VERSION}",
  "digest": null,
  "build_time_utc": "${BUILD_TIME}",
  "provenance_ref": null,
  "sbom_ref": null
}
EOF
echo "✅ RELEASE_MANIFEST.json updated (digest set to null for now)"

# 6. Final Verification (Local)
# Release preparation intentionally runs on a dirty tree after version/doc rendering,
# so use release-friendly gates that validate source truth without requiring the
# freshly generated artifacts to have been committed yet.
echo "🧪 Running release-friendly verification gates..."
make \
  verify-config \
  verify-doc-links \
  verify-doc-image-tags \
  verify-digest-lock \
  verify-release-output-contract \
  verify-compose-resolver \
  verify-systemd-runtime-contract \
  verify-installation-contract \
  verify-generated-artifacts-contract \
  verify-openapi-hard-mode \
  verify-embedded-webui-dist \
  || (echo "❌ Verification failed. Fix drift or errors." && exit 1)

echo "✨ Release preparation complete for ${TAG_VERSION}."
echo "📝 Please review and commit: backend/VERSION, RELEASE_MANIFEST.json, and generated docs."
