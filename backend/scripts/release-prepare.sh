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

# Keep every non-historical, pinned image example aligned with the release.
# The release verifier scans the same docs surface and fails on stale tags.
python3 - <<EOF
from pathlib import Path
import re

repo = Path("${REPO_ROOT}")
tag = "${TAG_VERSION}"
image_pattern = re.compile(
    r"ghcr\.io/manugh/xg2g:v[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?"
)
release_surfaces = [
    repo / "Dockerfile",
    repo / "backend/cmd/daemon/deploy/docker-compose.yml",
]
for candidate in (repo / "docs").rglob("*.md"):
    relative = candidate.relative_to(repo).as_posix()
    if relative.startswith("docs/release/"):
        continue
    if relative in {
        "docs/ops/RELEASE_OUTPUT_CONTRACT.md",
        "docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md",
    }:
        continue
    release_surfaces.append(candidate)

updated_paths = []
for path in release_surfaces:
    text = path.read_text()
    updated = image_pattern.sub(f"ghcr.io/manugh/xg2g:{tag}", text)
    if path == repo / "Dockerfile":
        updated = re.sub(
            r"(?m)^ARG BUILD_VERSION=v[^\s]+$",
            f"ARG BUILD_VERSION={tag}",
            updated,
            count=1,
        )
    if updated != text:
        path.write_text(updated)
        updated_paths.append(path.relative_to(repo).as_posix())

print(f"✅ synchronized {len(updated_paths)} pinned release surfaces to {tag}")
EOF

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
behavioral_changes="$(cat "docs/release/${TAG_VERSION}_behavioral_changes.txt")"
{
    printf '\n\n### Behavioral Changes (%s)\n' "${TAG_VERSION}"
    if [[ -n "${behavioral_changes}" ]]; then
        printf '%s\n' "${behavioral_changes}"
    fi
} >> "${REPO_ROOT}/CHANGELOG.md"

# 5. Update the checked-in release intent.
# The final commit SHA, build time, image digest, and provenance only exist in
# the tagged GitHub Actions run. Recording pre-commit guesses here creates
# false provenance, so those fields deliberately remain null in source.
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
  "git_sha": null,
  "image": "${IMAGE_REPO}",
  "tag": "${TAG_VERSION}",
  "digest": null,
  "build_time_utc": null,
  "provenance_ref": null,
  "sbom_ref": null
}
EOF
echo "✅ RELEASE_MANIFEST.json updated as release intent (final provenance is generated by GitHub)"

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
