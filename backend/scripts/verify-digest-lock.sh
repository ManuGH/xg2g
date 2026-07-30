#!/usr/bin/env bash
# Validate the committed release-intent and optional deployment digest pins.

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel)"
VERSION="$(tr -d '[:space:]' < "${REPO_ROOT}/backend/VERSION")"
LOCK_FILE="${REPO_ROOT}/DIGESTS.lock"
MANIFEST_FILE="${REPO_ROOT}/RELEASE_MANIFEST.json"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

[[ -f "${LOCK_FILE}" ]] || fail "DIGESTS.lock missing"
[[ -f "${MANIFEST_FILE}" ]] || fail "RELEASE_MANIFEST.json missing"

jq -e '
  (.image | type == "string" and length > 0) and
  (.releases | type == "object")
' "${LOCK_FILE}" >/dev/null ||
  fail "DIGESTS.lock is not valid release-digest JSON"

jq -e --arg version "${VERSION}" '
  .releases[$version] as $release
  | ($release | type == "object")
  and ($release.digest == "pending" or
       ($release.digest | type == "string" and test("^sha256:[a-f0-9]{64}$")))
  and ($release.published_at == "pending" or
       ($release.published_at | type == "string" and
        test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")))
' "${LOCK_FILE}" >/dev/null ||
  fail "DIGESTS.lock has no valid entry for ${VERSION}"

IMAGE_REPO="$(jq -er '.image' "${LOCK_FILE}")"
jq -e \
  --arg version "${VERSION}" \
  --arg image "${IMAGE_REPO}" '
    .version == $version and
    .tag == $version and
    .image == $image and
    (.git_sha == null or
     (.git_sha | type == "string" and test("^[a-f0-9]{40}$"))) and
    (.digest == null or
     (.digest | type == "string" and test("^sha256:[a-f0-9]{64}$"))) and
    (.build_time_utc == null or
     (.build_time_utc | type == "string" and
      test("^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$")))
  ' "${MANIFEST_FILE}" >/dev/null ||
  fail "RELEASE_MANIFEST.json does not match the ${VERSION} release intent"

DIGEST_VALUE="$(jq -r --arg version "${VERSION}" '.releases[$version].digest' "${LOCK_FILE}")"
if [[ "${DIGEST_VALUE}" == "pending" ]]; then
  echo "OK: ${VERSION} is structurally prepared; remote digest proof belongs to the tagged release workflow."
else
  echo "OK: ${VERSION} carries a well-formed deployment digest pin (${DIGEST_VALUE})."
fi
