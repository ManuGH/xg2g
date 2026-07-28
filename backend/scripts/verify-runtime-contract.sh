#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

repo_root="${tmp_dir}/repo"
fake_bin="${tmp_dir}/bin"
mkdir -p "${repo_root}/backend" "$fake_bin"
printf 'v3.8.1\n' > "${repo_root}/backend/VERSION"
printf '{"releases":{"v3.8.1":{"digest":"pending"}}}\n' > "${repo_root}/DIGESTS.lock"
printf '{"active_version":"v3.8.1"}\n' > "${tmp_dir}/runtime_state.json"

cat > "${fake_bin}/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

case "${1:-} ${2:-}" in
  "inspect --format")
    template="${3:-}"
    case "$template" in
      *State.Running*) printf 'true\n' ;;
      *Config.User*) printf '%s\n' "${FAKE_DOCKER_USER:-10001:10001}" ;;
      *State.Health*) printf '%s\n' "${FAKE_DOCKER_HEALTH:-healthy}" ;;
      *'.Image'*) printf 'sha256:test\n' ;;
      *) exit 2 ;;
    esac
    ;;
  "image inspect")
    template="${4:-}"
    case "$template" in
      *org.opencontainers.image.version*) printf '%s\n' "${FAKE_DOCKER_VERSION:-v3.8.1}" ;;
      *RepoDigests*) ;;
      *) exit 2 ;;
    esac
    ;;
  "exec xg2g")
    if [[ "${4:-}" == "--version" ]]; then
      printf '%s (commit: test, built: test)\n' "${FAKE_DOCKER_VERSION:-v3.8.1}"
      exit 0
    fi
    if [[ "${4:-}" == "healthcheck" ]]; then
      [[ "${FAKE_DOCKER_HEALTHCHECK_FAIL:-0}" != "1" ]]
      exit
    fi
    exit 2
    ;;
  *)
    exit 2
    ;;
esac
EOF
chmod +x "${fake_bin}/docker"

run_verifier() {
  PATH="${fake_bin}:${PATH}" \
    REPO_ROOT="$repo_root" \
    XG2G_RUNTIME_SNAPSHOT="${tmp_dir}/runtime_state.json" \
    "$SCRIPT_DIR/verify-runtime.sh"
}

run_verifier >/dev/null

if FAKE_DOCKER_USER=0:0 run_verifier >"${tmp_dir}/user-error" 2>&1; then
  echo "ERROR: runtime verifier accepted a root container" >&2
  exit 1
fi
grep -Fq "expected '10001:10001'" "${tmp_dir}/user-error"

if FAKE_DOCKER_HEALTHCHECK_FAIL=1 run_verifier >"${tmp_dir}/health-error" 2>&1; then
  echo "ERROR: runtime verifier accepted a failed live API probe" >&2
  exit 1
fi
grep -Fq "live API healthcheck failed" "${tmp_dir}/health-error"

if FAKE_DOCKER_VERSION=v3.7.2 run_verifier >"${tmp_dir}/version-error" 2>&1; then
  echo "ERROR: runtime verifier accepted a stale image version" >&2
  exit 1
fi
grep -Fq "expected 'v3.8.1'" "${tmp_dir}/version-error"

echo "OK: runtime verifier fails closed on user, health, and version drift."
