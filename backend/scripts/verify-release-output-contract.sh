#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"

DOC="${REPO_ROOT}/docs/ops/RELEASE_OUTPUT_CONTRACT.md"
GORELEASER_CFG="${REPO_ROOT}/.goreleaser.yml"
RELEASE_WORKFLOW="${REPO_ROOT}/.github/workflows/release.yml"
DOCKER_WORKFLOW="${REPO_ROOT}/.github/workflows/docker.yml"
FFMPEG_BASE_WORKFLOW="${REPO_ROOT}/.github/workflows/ffmpeg-base.yml"
RELEASE_DOCKERFILE="${REPO_ROOT}/infrastructure/docker/Dockerfile.release"
FFMPEG_BASE_DOCKERFILE="${REPO_ROOT}/Dockerfile.ffmpeg-base"
MK_VARIABLES="${REPO_ROOT}/mk/variables.mk"
FFMPEG_BUILD_SCRIPT="${REPO_ROOT}/backend/scripts/build-ffmpeg.sh"
RELEASE_PREPARE="${REPO_ROOT}/backend/scripts/release-prepare.sh"
RELEASE_VERIFY_REMOTE="${REPO_ROOT}/backend/scripts/release-verify-remote.sh"
DIGEST_VERIFY="${REPO_ROOT}/backend/scripts/verify-digest-lock.sh"
RELEASE_POLICY="${REPO_ROOT}/backend/scripts/verify-release-policy.sh"
DOC_IMAGE_TAGS="${REPO_ROOT}/backend/scripts/verify-doc-image-tags.sh"
RELEASE_INVARIANTS="${REPO_ROOT}/docs/ops/CONTRACT_INVARIANTS_RELEASE.md"

ALLOWED_GORELEASER_TOP_LEVEL_KEYS=(
  "version"
  "project_name"
  "before"
  "builds"
  "archives"
  "checksum"
  "snapshot"
  "changelog"
  "dockers"
  "docker_manifests"
  "release"
)

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

assert_file() {
  local file="$1"
  [[ -f "${file}" ]] || fail "missing required file: ${file}"
}

assert_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  grep -Fq -- "${needle}" "${file}" || fail "${label}: expected '${needle}' in ${file}"
}

assert_matches() {
  local file="$1"
  local pattern="$2"
  local label="$3"
  grep -Eq -- "${pattern}" "${file}" || fail "${label}: expected pattern '${pattern}' in ${file}"
}

assert_not_contains() {
  local file="$1"
  local needle="$2"
  local label="$3"
  if grep -Fq -- "${needle}" "${file}"; then
    fail "${label}: unexpected '${needle}' in ${file}"
  fi
}

normalize_tag_version() {
  local raw="${1:-}"
  local plain="${raw#v}"
  [[ -n "${plain}" ]] || fail "empty version"
  printf 'v%s\n' "${plain}"
}

normalize_plain_version() {
  local raw="${1:-}"
  local plain="${raw#v}"
  [[ -n "${plain}" ]] || fail "empty version"
  printf '%s\n' "${plain}"
}

extract_make_ffmpeg_version() {
  local version
  version="$(sed -n 's/^FFMPEG_VERSION := //p' "${MK_VARIABLES}" | head -n 1 | tr -d '[:space:]')"
  [[ -n "${version}" ]] || fail "unable to determine FFMPEG_VERSION from ${MK_VARIABLES}"
  printf '%s\n' "${version}"
}

extract_build_script_ffmpeg_version() {
  local version
  version="$(sed -n 's/^FFMPEG_VERSION=\"\(.*\)\"/\1/p' "${FFMPEG_BUILD_SCRIPT}" | head -n 1 | tr -d '[:space:]')"
  [[ -n "${version}" ]] || fail "unable to determine FFMPEG_VERSION from ${FFMPEG_BUILD_SCRIPT}"
  printf '%s\n' "${version}"
}

expected_bundle_files() {
  local version="$1"
  local plain
  plain="$(normalize_plain_version "${version}")"

  cat <<EOF
checksums.txt
xg2g_${plain}_darwin_amd64.tar.gz
xg2g_${plain}_darwin_arm64.tar.gz
xg2g_${plain}_linux_amd64.tar.gz
xg2g_${plain}_linux_arm64.tar.gz
xg2g_${plain}_windows_amd64.tar.gz
EOF
}

expected_archive_files() {
  expected_bundle_files "$1" | grep -v '^checksums.txt$'
}

assert_allowed_goreleaser_keys() {
  local key
  local allowed

  while IFS= read -r key; do
    [[ -n "${key}" ]] || continue
    allowed=0
    for allowed_key in "${ALLOWED_GORELEASER_TOP_LEVEL_KEYS[@]}"; do
      if [[ "${key}" == "${allowed_key}" ]]; then
        allowed=1
        break
      fi
    done
    [[ "${allowed}" -eq 1 ]] || fail "ungoverned goreleaser top-level stanza: ${key}"
  done < <(sed -n 's/^\([A-Za-z_][A-Za-z0-9_]*\):.*/\1/p' "${GORELEASER_CFG}")
}

verify_doc_contract() {
  assert_file "${DOC}"

  assert_contains "${DOC}" 'GitHub Release Asset Bundle' "release output doc section"
  assert_contains "${DOC}" 'Archive Payload Contract' "release output doc section"
  assert_contains "${DOC}" 'Registry Publication Outputs' "release output doc section"
  assert_contains "${DOC}" 'Non-Contract Outputs / Explicit Exclusions' "release output doc section"
  assert_contains "${DOC}" 'checksums.txt' "release output doc asset"
  assert_contains "${DOC}" 'xg2g_<version>_linux_amd64.tar.gz' "release output doc asset"
  assert_contains "${DOC}" 'ghcr.io/manugh/xg2g:vX.Y.Z-amd64' "release output doc registry tag"
  assert_contains "${DOC}" 'backend/scripts/verify-release-output-contract.sh' "release output doc verifier"
  assert_contains "${DOC}" 'backend/VERSION' "release output doc version source"
  assert_contains "${DOC}" 'RELEASE_MANIFEST.json' "release output doc exclusion"
  assert_contains "${DOC}" 'DIGESTS.lock' "release output doc exclusion"
  assert_contains "${DOC}" 'xg2g-ffmpeg:<ffmpeg-version>' "release output doc exclusion"
  assert_contains "${DOC}" '.github/workflows/ffmpeg-base.yml' "release output doc truth input"
  assert_contains "${DOC}" 'Dockerfile.ffmpeg-base' "release output doc truth input"
  assert_contains "${DOC}" 'unexpected published output' "release output doc policy"
}

verify_release_workflow_contract() {
  assert_file "${RELEASE_WORKFLOW}"
  assert_file "${DOCKER_WORKFLOW}"
  assert_file "${FFMPEG_BASE_WORKFLOW}"

  assert_contains "${RELEASE_WORKFLOW}" 'tags:' "release workflow trigger"
  assert_contains "${RELEASE_WORKFLOW}" '- "v*"' "release workflow tag trigger"
  assert_matches "${RELEASE_WORKFLOW}" 'goreleaser/goreleaser-action@([[:xdigit:]]{40}|v7)([[:space:]]*#.*v7)?' "release workflow goreleaser action"
  assert_contains "${RELEASE_WORKFLOW}" 'args: release --clean' "release workflow goreleaser args"
  assert_contains "${RELEASE_WORKFLOW}" 'Resolve FFmpeg base image reference' "release workflow ffmpeg base gate"
  assert_contains "${RELEASE_WORKFLOW}" 'docker/login-action@' "release workflow ghcr login"
  assert_contains "${DOCKER_WORKFLOW}" 'branches:' "docker workflow trigger"
  assert_contains "${DOCKER_WORKFLOW}" '- main' "docker workflow main trigger"
  assert_not_contains "${DOCKER_WORKFLOW}" '- "v*"' "docker workflow tag trigger"
  assert_contains "${FFMPEG_BASE_WORKFLOW}" 'Dockerfile.ffmpeg-base' "ffmpeg base workflow dockerfile"
  assert_contains "${FFMPEG_BASE_WORKFLOW}" 'ghcr.io/${{ github.repository_owner }}/xg2g-ffmpeg' "ffmpeg base workflow registry"
}

verify_goreleaser_contract() {
  assert_file "${GORELEASER_CFG}"
  assert_allowed_goreleaser_keys

  assert_contains "${GORELEASER_CFG}" 'project_name: xg2g' "goreleaser project"
  assert_contains "${GORELEASER_CFG}" 'formats: [tar.gz]' "goreleaser archive format"
  assert_contains "${GORELEASER_CFG}" '{{ .ProjectName }}_{{ .Version }}_{{ .Os }}_{{ .Arch }}' "goreleaser archive naming"
  assert_contains "${GORELEASER_CFG}" 'README.md' "goreleaser archive payload"
  assert_contains "${GORELEASER_CFG}" 'LICENSE' "goreleaser archive payload"
  assert_contains "${GORELEASER_CFG}" 'backend/VERSION' "goreleaser archive payload"
  assert_contains "${GORELEASER_CFG}" 'docs/**' "goreleaser archive payload"
  assert_contains "${GORELEASER_CFG}" 'name_template: "checksums.txt"' "goreleaser checksum naming"
  assert_contains "${GORELEASER_CFG}" 'ghcr.io/manugh/xg2g:{{ .Tag }}-amd64' "goreleaser amd64 image tag"
  assert_contains "${GORELEASER_CFG}" 'ghcr.io/manugh/xg2g:{{ .Tag }}-arm64' "goreleaser arm64 image tag"
  assert_contains "${GORELEASER_CFG}" 'name_template: "ghcr.io/manugh/xg2g:{{ .Tag }}"' "goreleaser manifest tag"
  assert_contains "${GORELEASER_CFG}" 'name_template: "ghcr.io/manugh/xg2g:latest"' "goreleaser latest manifest"
  assert_not_contains "${GORELEASER_CFG}" 'build-ffmpeg.sh' "goreleaser release ffmpeg source build"
}

verify_release_docker_contract() {
  local mk_ffmpeg_version
  local build_script_ffmpeg_version

  assert_file "${RELEASE_DOCKERFILE}"
  assert_file "${FFMPEG_BASE_DOCKERFILE}"
  assert_file "${MK_VARIABLES}"
  assert_file "${FFMPEG_BUILD_SCRIPT}"

  mk_ffmpeg_version="$(extract_make_ffmpeg_version)"
  build_script_ffmpeg_version="$(extract_build_script_ffmpeg_version)"
  [[ "${mk_ffmpeg_version}" == "${build_script_ffmpeg_version}" ]] || fail "FFMPEG_VERSION drift between ${MK_VARIABLES} (${mk_ffmpeg_version}) and ${FFMPEG_BUILD_SCRIPT} (${build_script_ffmpeg_version})"

  assert_contains "${RELEASE_DOCKERFILE}" "ARG FFMPEG_BASE_IMAGE=ghcr.io/manugh/xg2g-ffmpeg:${mk_ffmpeg_version}" "release docker ffmpeg base image"
  assert_contains "${RELEASE_DOCKERFILE}" 'FROM ${FFMPEG_BASE_IMAGE} AS runtime' "release docker runtime base"
  assert_not_contains "${RELEASE_DOCKERFILE}" 'RUN ./build-ffmpeg.sh' "release docker ffmpeg rebuild"
  assert_contains "${FFMPEG_BASE_DOCKERFILE}" 'COPY backend/scripts/build-ffmpeg.sh .' "ffmpeg base docker build script"
  assert_contains "${FFMPEG_BASE_DOCKERFILE}" 'COPY --chown=root:root backend/scripts/ffmpeg-wrapper.sh /usr/local/bin/ffmpeg' "ffmpeg base docker wrapper"
}

verify_release_input_contract() {
  assert_contains "${RELEASE_PREPARE}" 'backend/VERSION' "release prepare version source"
  assert_contains "${RELEASE_PREPARE}" 'docs/release/' "release prepare behavioral changes path"
  assert_contains "${RELEASE_VERIFY_REMOTE}" 'backend/VERSION' "release verify remote version source"
  assert_contains "${DIGEST_VERIFY}" 'backend/VERSION' "verify-digest-lock version source"
  assert_contains "${RELEASE_POLICY}" 'backend/VERSION' "release policy allowlist"
  assert_contains "${DOC_IMAGE_TAGS}" 'backend/VERSION' "verify-doc-image-tags version fallback"
  assert_contains "${RELEASE_INVARIANTS}" 'backend/VERSION' "release invariants version source"
}

compare_exact_file_set() {
  local actual_file="$1"
  local expected_file="$2"

  cmp -s "${actual_file}" "${expected_file}" && return 0

  echo "Actual file set:" >&2
  cat "${actual_file}" >&2
  echo "Expected file set:" >&2
  cat "${expected_file}" >&2
  fail "release bundle file set drift detected"
}

verify_archive_payload() {
  local archive="$1"
  local version="$2"
  local os="$3"
  local entries
  local version_member
  local archived_version
  local expected_tag

  entries="$(tar -tzf "${archive}")" || fail "unable to list archive: ${archive}"
  expected_tag="$(normalize_tag_version "${version}")"

  if [[ "${os}" == "windows" ]]; then
    printf '%s\n' "${entries}" | grep -Eq '(^|/)xg2g\.exe$' || fail "archive missing xg2g.exe: ${archive}"
  else
    printf '%s\n' "${entries}" | grep -Eq '(^|/)xg2g$' || fail "archive missing xg2g binary: ${archive}"
  fi

  printf '%s\n' "${entries}" | grep -Eq '(^|/)README\.md$' || fail "archive missing README.md: ${archive}"
  printf '%s\n' "${entries}" | grep -Eq '(^|/)LICENSE$' || fail "archive missing LICENSE: ${archive}"
  printf '%s\n' "${entries}" | grep -Eq '(^|/)backend/VERSION$' || fail "archive missing backend/VERSION: ${archive}"
  printf '%s\n' "${entries}" | grep -Eq '(^|/)docs/.+' || fail "archive missing docs payload: ${archive}"

  version_member="$(printf '%s\n' "${entries}" | grep -E '(^|/)backend/VERSION$' | head -n 1 || true)"
  [[ -n "${version_member}" ]] || fail "archive missing backend/VERSION member: ${archive}"

  archived_version="$(tar -xOf "${archive}" "${version_member}" | tr -d '[:space:]')"
  [[ "${archived_version}" == "${expected_tag}" ]] || fail "archive backend/VERSION drift in ${archive}: expected ${expected_tag}, got ${archived_version}"
}

assert_release_bundle_dir() {
  local bundle_dir="$1"
  local version="$2"
  local tmpdir
  local actual_files
  local expected_files
  local actual_checksums
  local expected_archives
  local archive
  local archive_name
  local archive_os

  [[ -d "${bundle_dir}" ]] || fail "bundle dir does not exist: ${bundle_dir}"

  tmpdir="$(mktemp -d)"
  actual_files="${tmpdir}/actual-files.txt"
  expected_files="${tmpdir}/expected-files.txt"
  actual_checksums="${tmpdir}/actual-checksums.txt"
  expected_archives="${tmpdir}/expected-archives.txt"

  find "${bundle_dir}" -maxdepth 1 -mindepth 1 -type f -exec basename '{}' ';' | LC_ALL=C sort > "${actual_files}"
  expected_bundle_files "${version}" | LC_ALL=C sort > "${expected_files}"
  compare_exact_file_set "${actual_files}" "${expected_files}"

  expected_archive_files "${version}" | LC_ALL=C sort > "${expected_archives}"
  awk 'NF >= 2 {print $2}' "${bundle_dir}/checksums.txt" | sed 's/^\*//' | LC_ALL=C sort > "${actual_checksums}"
  compare_exact_file_set "${actual_checksums}" "${expected_archives}"

  while IFS= read -r archive_name; do
    [[ -n "${archive_name}" ]] || continue
    archive="${bundle_dir}/${archive_name}"
    case "${archive_name}" in
      *_linux_*.tar.gz) archive_os="linux" ;;
      *_darwin_*.tar.gz) archive_os="darwin" ;;
      *_windows_*.tar.gz) archive_os="windows" ;;
      *) rm -rf "${tmpdir}"; fail "unexpected archive naming: ${archive_name}" ;;
    esac
    verify_archive_payload "${archive}" "${version}" "${archive_os}"
  done < "${expected_archives}"

  rm -rf "${tmpdir}"
}

create_synthetic_bundle() {
  local bundle_dir="$1"
  local version="$2"
  local include_rogue="$3"
  local tag_version
  local plain_version
  local archive_name
  local payload_root
  local binary_name
  local os
  local arch

  tag_version="$(normalize_tag_version "${version}")"
  plain_version="$(normalize_plain_version "${version}")"

  mkdir -p "${bundle_dir}"

  while IFS=: read -r os arch; do
    archive_name="xg2g_${plain_version}_${os}_${arch}.tar.gz"
    payload_root="${bundle_dir}/payload-${os}-${arch}"
    mkdir -p "${payload_root}/backend" "${payload_root}/docs/ops"
    printf '%s\n' 'synthetic release readme' > "${payload_root}/README.md"
    printf '%s\n' 'synthetic license' > "${payload_root}/LICENSE"
    printf '%s\n' "${tag_version}" > "${payload_root}/backend/VERSION"
    printf '%s\n' 'synthetic docs' > "${payload_root}/docs/ops/placeholder.md"

    if [[ "${os}" == "windows" ]]; then
      binary_name="xg2g.exe"
    else
      binary_name="xg2g"
    fi

    printf '%s\n' 'binary' > "${payload_root}/${binary_name}"
    tar -czf "${bundle_dir}/${archive_name}" -C "${payload_root}" .
    rm -rf "${payload_root}"
  done <<'EOF'
linux:amd64
linux:arm64
darwin:amd64
darwin:arm64
windows:amd64
EOF

  (
    cd "${bundle_dir}"
    sha256sum xg2g_*.tar.gz > checksums.txt
  )

  if [[ "${include_rogue}" == "true" ]]; then
    printf '%s\n' 'unexpected output' > "${bundle_dir}/rogue.txt"
  fi
}

verify_synthetic_bundle_guards() {
  local tmp_root
  local positive_dir
  local negative_dir
  local sample_version="v9.9.9"

  tmp_root="$(mktemp -d)"
  positive_dir="${tmp_root}/positive"
  negative_dir="${tmp_root}/negative"

  create_synthetic_bundle "${positive_dir}" "${sample_version}" "false"
  "${BASH_SOURCE[0]}" --assert-bundle-dir "${positive_dir}" "${sample_version}" >/dev/null

  create_synthetic_bundle "${negative_dir}" "${sample_version}" "true"
  if "${BASH_SOURCE[0]}" --assert-bundle-dir "${negative_dir}" "${sample_version}" >/dev/null 2>&1; then
    rm -rf "${tmp_root}"
    fail "negative guard failed: unexpected release output passed bundle verification"
  fi

  rm -rf "${tmp_root}"
}

main() {
  case "${1:-}" in
    --assert-bundle-dir)
      [[ "$#" -eq 3 ]] || fail "usage: $0 --assert-bundle-dir <dir> <version>"
      assert_release_bundle_dir "$2" "$3"
      echo "OK: release bundle contract holds."
      return 0
      ;;
    --verify-bundle-dir)
      [[ "$#" -eq 4 && "$3" == "--version" ]] || fail "usage: $0 --verify-bundle-dir <dir> --version <tag>"
      assert_release_bundle_dir "$2" "$4"
      echo "OK: release bundle contract holds."
      return 0
      ;;
    "")
      ;;
    *)
      fail "usage: $0 [--verify-bundle-dir <dir> --version <tag>]"
      ;;
  esac

  verify_doc_contract
  verify_release_workflow_contract
  verify_goreleaser_contract
  verify_release_docker_contract
  verify_release_input_contract
  verify_synthetic_bundle_guards

  echo "OK: release output contract holds."
}

main "$@"
