#!/usr/bin/env bash

set -euo pipefail

BASE_URL="${1:-${XG2G_BASE_URL:-}}"
API_TOKEN="${2:-${XG2G_API_TOKEN:-}}"

if [[ -z "${BASE_URL}" ]]; then
  echo "Usage: $0 <public-base-url> [api-token]"
  echo "   or: XG2G_BASE_URL=https://tv.example.net XG2G_API_TOKEN=... $0"
  exit 1
fi

if [[ -z "${API_TOKEN}" ]]; then
  echo "FAIL: API token required for /api/v3/system/connectivity"
  echo "Set XG2G_API_TOKEN or pass it as the second argument."
  exit 1
fi

for cmd in curl jq mktemp; do
  command -v "${cmd}" >/dev/null || {
    echo "FAIL: required command not found: ${cmd}"
    exit 1
  }
done

BASE_URL="${BASE_URL%/}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

fetch_json() {
  local label="$1"
  local url="$2"
  local body_file="${TMP_DIR}/${label}.json"
  local status

  status="$(
    curl -sS \
      -H "Authorization: Bearer ${API_TOKEN}" \
      -o "${body_file}" \
      -w "%{http_code}" \
      "${url}"
  )"

  if [[ "${status}" != "200" ]]; then
    echo "FAIL: ${label} returned HTTP ${status}"
    cat "${body_file}"
    echo
    exit 1
  fi

  echo "${body_file}"
}

fetch_public_json() {
  local label="$1"
  local url="$2"
  local body_file="${TMP_DIR}/${label}.json"
  local status

  status="$(
    curl -sS \
      -o "${body_file}" \
      -w "%{http_code}" \
      "${url}"
  )"

  if [[ "${status}" != "200" ]]; then
    echo "FAIL: ${label} returned HTTP ${status}"
    cat "${body_file}"
    echo
    exit 1
  fi

  echo "${body_file}"
}

assert_jq() {
  local file="$1"
  local expression="$2"
  local message="$3"

  if ! jq -e "${expression}" "${file}" >/dev/null; then
    echo "FAIL: ${message}"
    jq . "${file}"
    exit 1
  fi
}

echo "Verifying public deployment at ${BASE_URL}"

healthz_file="$(fetch_public_json healthz "${BASE_URL}/healthz")"
readyz_file="$(fetch_public_json readyz "${BASE_URL}/readyz?verbose=true")"
connectivity_file="$(fetch_json connectivity "${BASE_URL}/api/v3/system/connectivity")"

assert_jq "${healthz_file}" '.status == "healthy"' "healthz must report status=healthy"
assert_jq "${readyz_file}" '.ready == true and (.status == "healthy" or .status == "degraded")' "readyz must report ready=true"
assert_jq "${connectivity_file}" '.status == "ok" or .status == "warn"' "connectivity contract must be ok or warn"
assert_jq "${connectivity_file}" '.startupFatal == false' "connectivity startup must not be fatal"
assert_jq "${connectivity_file}" '.readinessBlocked == false' "connectivity readiness must not be blocked"
assert_jq "${connectivity_file}" '.pairingBlocked == false' "connectivity pairing must not be blocked"
assert_jq "${connectivity_file}" '.webBlocked == false' "connectivity web bootstrap must not be blocked"
assert_jq "${connectivity_file}" '.request.effectiveHttps == true' "public request evaluation must resolve to HTTPS"
assert_jq "${connectivity_file}" '.public == true' "deployment profile must be public for this smoke"
assert_jq "${connectivity_file}" '(.allowedOrigins | index("*")) == null' "public deployment must not allow wildcard browser origins"
assert_jq "${connectivity_file}" '[.publishedEndpoints[] | select(.kind != "public_https" and .allowWeb == true)] | length == 0' "public deployment must not expose local endpoints to web clients"
assert_jq "${connectivity_file}" '(.selections.webPublic.endpoint.kind == "public_https") and (.selections.webPublic.endpoint.url | startswith("https://"))' "public web endpoint must be HTTPS"
assert_jq "${connectivity_file}" '(.selections.pairingPublic.endpoint.kind == "public_https") and (.selections.pairingPublic.endpoint.url | startswith("https://"))' "public pairing endpoint must be HTTPS"
assert_jq "${connectivity_file}" '(.selections.nativePublic.endpoint.kind == "public_https") and (.selections.nativePublic.endpoint.url | startswith("https://"))' "public native endpoint must be HTTPS"

web_origin="$(jq -r '.selections.webPublic.endpoint.url' "${connectivity_file}" | sed -E 's#^(https?://[^/]+).*#\1#')"
if ! jq -e --arg origin "${web_origin}" '.allowedOrigins | index($origin)' "${connectivity_file}" >/dev/null; then
  echo "FAIL: allowedOrigins must contain selected public web origin ${web_origin}"
  jq . "${connectivity_file}"
  exit 1
fi

echo "Connectivity summary:"
jq '{
  profile,
  status,
  effectiveHttps: .request.effectiveHttps,
  trustedProxyMatch: .request.trustedProxyMatch,
  allowedOrigins,
  webPublic: .selections.webPublic.endpoint.url,
  pairingPublic: .selections.pairingPublic.endpoint.url,
  nativePublic: .selections.nativePublic.endpoint.url
}' "${connectivity_file}"

echo "PASS: public deployment contract verified"
