#!/usr/bin/env bash
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VENV_DIR="${REPO_ROOT}/.venv"
REQ_FILE="${REPO_ROOT}/scripts/requirements-python-tools.txt"
STAMP_FILE="${VENV_DIR}/.xg2g-python-tools.sha256"

if ! command -v python3 >/dev/null 2>&1; then
	echo "❌ python3 not found. Install Python 3 first."
	exit 1
fi

if [ ! -d "${VENV_DIR}" ]; then
	echo "Creating ${VENV_DIR} ..."
	python3 -m venv "${VENV_DIR}"
fi

VENV_PY="${VENV_DIR}/bin/python3"
if [ ! -x "${VENV_PY}" ]; then
	echo "❌ Missing ${VENV_PY}; venv creation failed."
	exit 1
fi

REQ_HASH="$(shasum -a 256 "${REQ_FILE}" | awk '{print $1}')"
INSTALLED_HASH=""
if [ -f "${STAMP_FILE}" ]; then
	INSTALLED_HASH="$(cat "${STAMP_FILE}")"
fi

if [ "${REQ_HASH}" != "${INSTALLED_HASH}" ]; then
	echo "Installing pinned Python tooling requirements ..."
	"${VENV_PY}" -m pip --disable-pip-version-check install --quiet --requirement "${REQ_FILE}"
	printf '%s\n' "${REQ_HASH}" > "${STAMP_FILE}"
fi

echo "✅ Python toolchain ready: ${VENV_PY}"
