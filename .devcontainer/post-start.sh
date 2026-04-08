#!/usr/bin/env bash
set -euo pipefail

SOCKET="/var/run/docker.sock"

if [[ ! -S "${SOCKET}" ]]; then
  exit 0
fi

socket_gid="$(stat -c '%g' "${SOCKET}")"
group_name="$(getent group "${socket_gid}" | cut -d: -f1 || true)"

if [[ -z "${group_name}" ]]; then
  group_name="docker-host"
  if getent group "${group_name}" >/dev/null 2>&1; then
    existing_gid="$(getent group "${group_name}" | cut -d: -f3)"
    if [[ "${existing_gid}" != "${socket_gid}" ]]; then
      sudo groupmod -g "${socket_gid}" "${group_name}"
    fi
  else
    sudo groupadd --gid "${socket_gid}" "${group_name}"
  fi
fi

if id -nG "${USER}" | tr ' ' '\n' | grep -Fxq "${group_name}"; then
  exit 0
fi

sudo usermod -aG "${group_name}" "${USER}"
echo "NOTE: Added ${USER} to ${group_name} for Docker socket access. Reopen the terminal if 'docker' still reports a permission error."
