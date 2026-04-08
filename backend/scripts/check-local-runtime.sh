#!/usr/bin/env bash
set -euo pipefail

MODE="${1:-base}"

fail() {
  echo "ERROR: $*" >&2
  exit 1
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || fail "missing required command: $1"
}

check_docker_runtime() {
  need_cmd docker
  docker info >/dev/null 2>&1 || fail "Docker daemon not reachable. Start Docker, Colima, or OrbStack before running local container targets."
  docker compose version >/dev/null 2>&1 || fail "Docker Compose plugin not available. Install or enable it before running local container targets."
}

check_render_nodes() {
  compgen -G "/dev/dri/renderD*" >/dev/null || fail "No /dev/dri/renderD* devices are visible. Use 'make start' for CPU-only, or expose a render node before 'make start-gpu'."
}

main() {
  check_docker_runtime

  case "${MODE}" in
    base)
      ;;
    vaapi)
      check_render_nodes
      ;;
    nvidia)
      echo "NOTE: NVIDIA capability itself is validated when the container starts."
      ;;
    *)
      fail "unknown runtime mode: ${MODE}"
      ;;
  esac

  echo "✅ Local container runtime checks passed."
}

main "$@"
