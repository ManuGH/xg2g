#!/usr/bin/env bash
# Resolve and verify an xg2g OCI release directly through the registry API.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/backend/VERSION")"
LOCK_FILE="${REPO_ROOT}/DIGESTS.lock"
IMAGE_REPO="$(jq -er '.image' "${LOCK_FILE}")"
ALSO_TAG=""
OUTPUT_JSON=false
REQUIRED_PLATFORMS=()

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

usage() {
  cat <<'EOF'
Usage: release-verify-remote.sh [options]

Options:
  --version <tag>              OCI tag to verify (default: backend/VERSION)
  --also-tag <tag>             Require this tag to resolve to the same digest
  --require-platform <os/arch> Require a platform in the OCI index (repeatable)
  --json                       Emit machine-readable JSON only
  -h, --help                   Show this help
EOF
}

while [[ "$#" -gt 0 ]]; do
  case "$1" in
    --version)
      [[ "$#" -ge 2 ]] || fail "--version requires a value"
      VERSION="$2"
      shift 2
      ;;
    --also-tag)
      [[ "$#" -ge 2 ]] || fail "--also-tag requires a value"
      ALSO_TAG="$2"
      shift 2
      ;;
    --require-platform)
      [[ "$#" -ge 2 ]] || fail "--require-platform requires a value"
      REQUIRED_PLATFORMS+=("$2")
      shift 2
      ;;
    --json)
      OUTPUT_JSON=true
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      fail "unknown argument: $1"
      ;;
  esac
done

[[ "${IMAGE_REPO}" == */* ]] || fail "invalid image repository: ${IMAGE_REPO}"
REGISTRY_HOST="${IMAGE_REPO%%/*}"
REGISTRY_REPOSITORY="${IMAGE_REPO#*/}"
[[ "${REGISTRY_HOST}" == "ghcr.io" || -n "${XG2G_REGISTRY_API_BASE:-}" ]] ||
  fail "unsupported registry host without XG2G_REGISTRY_API_BASE: ${REGISTRY_HOST}"

REGISTRY_API_BASE="${XG2G_REGISTRY_API_BASE:-https://${REGISTRY_HOST}}"
TOKEN_URL="${XG2G_REGISTRY_TOKEN_URL:-https://${REGISTRY_HOST}/token?scope=repository:${REGISTRY_REPOSITORY}:pull}"
ACCEPT_HEADER="application/vnd.oci.image.index.v1+json, application/vnd.docker.distribution.manifest.list.v2+json"
TMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TMP_ROOT}"' EXIT

TOKEN="$(
  curl --connect-timeout 10 --max-time 30 --retry 3 --retry-all-errors -fsSL "${TOKEN_URL}" |
    jq -er '.token // .access_token'
)" || fail "unable to obtain a registry pull token"

fetch_manifest() {
  local reference="$1"
  local body_file="$2"
  local header_file="$3"

  curl \
    --connect-timeout 10 \
    --max-time 60 \
    --retry 3 \
    --retry-all-errors \
    -fsSL \
    -D "${header_file}" \
    -o "${body_file}" \
    -H "Authorization: Bearer ${TOKEN}" \
    -H "Accept: ${ACCEPT_HEADER}" \
    "${REGISTRY_API_BASE}/v2/${REGISTRY_REPOSITORY}/manifests/${reference}"
}

manifest_digest() {
  local header_file="$1"
  tr -d '\r' < "${header_file}" |
    awk 'tolower($1) == "docker-content-digest:" { print $2 }' |
    tail -n 1
}

VERSION_BODY="${TMP_ROOT}/version-manifest.json"
VERSION_HEADERS="${TMP_ROOT}/version-headers.txt"
fetch_manifest "${VERSION}" "${VERSION_BODY}" "${VERSION_HEADERS}" ||
  fail "image ${IMAGE_REPO}:${VERSION} is not reachable"

DIGEST="$(manifest_digest "${VERSION_HEADERS}")"
[[ "${DIGEST}" =~ ^sha256:[a-f0-9]{64}$ ]] ||
  fail "registry returned an invalid digest for ${IMAGE_REPO}:${VERSION}: ${DIGEST:-missing}"

jq -e '
  .schemaVersion == 2 and
  (.mediaType == "application/vnd.oci.image.index.v1+json" or
   .mediaType == "application/vnd.docker.distribution.manifest.list.v2+json") and
  (.manifests | type == "array")
' "${VERSION_BODY}" >/dev/null ||
  fail "${IMAGE_REPO}:${VERSION} is not a supported multi-platform OCI index"

for platform in "${REQUIRED_PLATFORMS[@]}"; do
  platform_os="${platform%%/*}"
  platform_arch="${platform#*/}"
  [[ "${platform_os}" != "${platform_arch}" && -n "${platform_os}" && -n "${platform_arch}" ]] ||
    fail "invalid platform: ${platform}"
  jq -e \
    --arg os "${platform_os}" \
    --arg arch "${platform_arch}" \
    'any(.manifests[]; .platform.os == $os and .platform.architecture == $arch)' \
    "${VERSION_BODY}" >/dev/null ||
    fail "${IMAGE_REPO}:${VERSION} is missing required platform ${platform}"
done

if [[ -n "${ALSO_TAG}" ]]; then
  ALSO_BODY="${TMP_ROOT}/also-manifest.json"
  ALSO_HEADERS="${TMP_ROOT}/also-headers.txt"
  fetch_manifest "${ALSO_TAG}" "${ALSO_BODY}" "${ALSO_HEADERS}" ||
    fail "image ${IMAGE_REPO}:${ALSO_TAG} is not reachable"
  ALSO_DIGEST="$(manifest_digest "${ALSO_HEADERS}")"
  [[ "${ALSO_DIGEST}" == "${DIGEST}" ]] ||
    fail "${IMAGE_REPO}:${VERSION} (${DIGEST}) and ${IMAGE_REPO}:${ALSO_TAG} (${ALSO_DIGEST:-missing}) differ"
fi

PLATFORMS="$(
  jq -c '
    [
      .manifests[]
      | select(.platform.os != "unknown" and .platform.architecture != "unknown")
      | "\(.platform.os)/\(.platform.architecture)"
    ]
    | unique
  ' "${VERSION_BODY}"
)"

if [[ "${OUTPUT_JSON}" == "true" ]]; then
  jq -n \
    --arg image "${IMAGE_REPO}" \
    --arg tag "${VERSION}" \
    --arg digest "${DIGEST}" \
    --arg also_tag "${ALSO_TAG}" \
    --argjson platforms "${PLATFORMS}" \
    '{
      image: $image,
      tag: $tag,
      digest: $digest,
      platforms: $platforms,
      equivalent_tag: (if $also_tag == "" then null else $also_tag end)
    }'
else
  echo "Verified ${IMAGE_REPO}:${VERSION}@${DIGEST}"
  echo "Platforms: $(jq -r 'join(", ")' <<<"${PLATFORMS}")"
  if [[ -n "${ALSO_TAG}" ]]; then
    echo "Equivalent tag: ${ALSO_TAG}"
  fi
fi
