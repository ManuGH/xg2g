#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_PATH="${ROOT}/bin/xg2g"
ENV_FILE="${ROOT}/.env"
BUILD_DIR="${ROOT}/build"
BACKEND_LOG="${ROOT}/logs/android-local-backend.log"
BASE_URL="http://10.0.2.2:8080/ui/"
PACKAGE_NAME="io.github.manugh.xg2g.android.dev"
MAIN_ACTIVITY="${PACKAGE_NAME}/io.github.manugh.xg2g.android.MainActivity"

mkdir -p "${ROOT}/logs" \
    "${BUILD_DIR}/dev-store" \
    "${BUILD_DIR}/dev-hls" \
    "${BUILD_DIR}/dev-nfs-recordings"

if [[ ! -f "${ENV_FILE}" ]]; then
    echo "Missing ${ENV_FILE}. Create it first (for example from .env.example)." >&2
    exit 1
fi

if ! command -v adb >/dev/null 2>&1; then
    echo "adb is required for Android TV launch support." >&2
    exit 1
fi

# shellcheck disable=SC1090
set -a
. "${ENV_FILE}"
set +a

if [[ -z "${XG2G_API_TOKEN:-}" ]]; then
    echo "XG2G_API_TOKEN must be set in ${ENV_FILE}." >&2
    exit 1
fi

cleanup() {
    if [[ -n "${backend_pid:-}" ]]; then
        kill "${backend_pid}" 2>/dev/null || true
        wait "${backend_pid}" 2>/dev/null || true
    fi
}

trap cleanup EXIT INT TERM

echo "Building embedded WebUI bundle..."
make ui-build

echo "Building local backend binary..."
(
    cd "${ROOT}/backend"
    GOWORK=off go build -trimpath -buildvcs=false -mod=vendor -o "${BIN_PATH}" ./cmd/daemon
)

echo "Starting local backend on http://127.0.0.1:8080 ..."
(
    export XG2G_LISTEN=":8080"
    exec "${BIN_PATH}"
) >> "${BACKEND_LOG}" 2>&1 &
backend_pid=$!

for _ in $(seq 1 40); do
    if curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
        break
    fi
    sleep 0.5
done

if ! curl -fsS --max-time 2 http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
    echo "Local backend did not become ready. See ${BACKEND_LOG}." >&2
    exit 1
fi

echo "Backend ready."
echo "UI: http://127.0.0.1:8080/ui/"
echo "Logs: ${BACKEND_LOG}"

adb_device_count="$(adb devices | awk 'NR > 1 && $2 == "device" {count++} END {print count + 0}')"
if [[ "${adb_device_count}" -gt 0 ]]; then
    if adb shell pm path "${PACKAGE_NAME}" >/dev/null 2>&1; then
        echo "Launching Android dev app against ${BASE_URL} ..."
        adb shell am force-stop "${PACKAGE_NAME}" >/dev/null 2>&1 || true
        adb shell am start \
            -n "${MAIN_ACTIVITY}" \
            --es base_url "${BASE_URL}" \
            --es auth_token "${XG2G_API_TOKEN}" >/dev/null
        echo "Android app launched."
    else
        echo "adb device detected, but ${PACKAGE_NAME} is not installed."
        echo "Install the Android dev build first, then rerun this helper."
    fi
else
    echo "No adb device detected. Start the Android app manually with:"
    echo "adb shell am start -n ${MAIN_ACTIVITY} --es base_url ${BASE_URL} --es auth_token <token>"
fi

echo "Press Ctrl+C to stop the local backend."
wait "${backend_pid}"
