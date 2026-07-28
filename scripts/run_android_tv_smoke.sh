#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${ROOT}/.env"
BIN_PATH="${ROOT}/bin/xg2g"
LOG_DIR="${ROOT}/logs/android-tv-smoke"
BACKEND_LOG="${LOG_DIR}/backend.log"
LOGCAT_LOG="${LOG_DIR}/logcat.txt"
EMULATOR_LOG="${LOG_DIR}/emulator.log"
UI_DUMP_PATH="/sdcard/xg2g-tv-smoke-window.xml"
TV_SMOKE_PORT="${XG2G_TV_SMOKE_PORT:-8080}"
LOCAL_HTTP_BASE="http://127.0.0.1:${TV_SMOKE_PORT}"
EMULATOR_UI_BASE="http://10.0.2.2:${TV_SMOKE_PORT}/ui/"
LOCAL_UI_REFERER="${LOCAL_HTTP_BASE}/ui/"
SMOKE_RUNTIME_ROOT="${ROOT}/build/android-tv-smoke-${TV_SMOKE_PORT}"
SMOKE_STORE_PATH="${SMOKE_RUNTIME_ROOT}/store"
SMOKE_HLS_ROOT="${SMOKE_RUNTIME_ROOT}/hls"
PACKAGE_NAME="io.github.manugh.xg2g.android.dev"
MAIN_ACTIVITY="${PACKAGE_NAME}/io.github.manugh.xg2g.android.MainActivity"
DEFAULT_AVD="Television_4K"
SCREENSHOT_DIR="${LOG_DIR}/screens"
UI_DIR="${LOG_DIR}/ui"

mkdir -p "${LOG_DIR}" "${SCREENSHOT_DIR}" "${UI_DIR}" "${SMOKE_RUNTIME_ROOT}"
: > "${BACKEND_LOG}"
: > "${LOGCAT_LOG}"

backend_pid=""
logcat_pid=""
started_emulator=0
selected_serial=""

log() {
    printf '[android-tv-smoke] %s\n' "$*"
}

fail() {
    log "ERROR: $*"
    exit 1
}

require_cmd() {
    if ! command -v "$1" >/dev/null 2>&1; then
        fail "missing required command: $1"
    fi
}

port_is_open() {
    python3 - "$1" <<'PY'
import socket
import sys

port = int(sys.argv[1])
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as sock:
    sock.settimeout(0.25)
    sys.exit(0 if sock.connect_ex(("127.0.0.1", port)) == 0 else 1)
PY
}

cleanup() {
    local exit_code=$?

    if [[ -n "${logcat_pid}" ]]; then
        kill "${logcat_pid}" 2>/dev/null || true
        wait "${logcat_pid}" 2>/dev/null || true
    fi

    if [[ -n "${backend_pid}" ]]; then
        kill "${backend_pid}" 2>/dev/null || true
        wait "${backend_pid}" 2>/dev/null || true
    fi

    if (( exit_code == 0 )); then
        log "OK: Android TV smoke passed."
        log "Artifacts: ${LOG_DIR}"
    else
        log "Smoke failed. Artifacts: ${LOG_DIR}"
        if [[ -f "${BACKEND_LOG}" ]]; then
            log "Backend tail:"
            tail -n 40 "${BACKEND_LOG}" || true
        fi
    fi

    exit "${exit_code}"
}

trap cleanup EXIT INT TERM

adb_cmd() {
    adb -s "${selected_serial}" "$@"
}

adb_shell() {
    adb_cmd shell "$@"
}

find_emulator_bin() {
    if command -v emulator >/dev/null 2>&1; then
        command -v emulator
        return 0
    fi

    local candidates=(
        "${ANDROID_SDK_ROOT:-}/emulator/emulator"
        "${ANDROID_HOME:-}/emulator/emulator"
        "${HOME}/Library/Android/sdk/emulator/emulator"
    )
    local candidate
    for candidate in "${candidates[@]}"; do
        if [[ -x "${candidate}" ]]; then
            printf '%s\n' "${candidate}"
            return 0
        fi
    done
    return 1
}

list_connected_devices() {
    adb devices | awk 'NR > 1 && $2 == "device" {print $1}'
}

choose_serial() {
    if [[ -n "${ANDROID_SERIAL:-}" ]]; then
        printf '%s\n' "${ANDROID_SERIAL}"
        return 0
    fi

    mapfile -t devices < <(list_connected_devices)
    if (( ${#devices[@]} == 1 )); then
        printf '%s\n' "${devices[0]}"
        return 0
    fi

    if (( ${#devices[@]} == 0 )); then
        return 1
    fi

    local emulator_device=""
    local device
    for device in "${devices[@]}"; do
        if [[ "${device}" == emulator-* ]]; then
            if [[ -n "${emulator_device}" ]]; then
                fail "multiple adb devices detected; set ANDROID_SERIAL explicitly"
            fi
            emulator_device="${device}"
        fi
    done
    if [[ -n "${emulator_device}" ]]; then
        printf '%s\n' "${emulator_device}"
        return 0
    fi

    fail "multiple adb devices detected; set ANDROID_SERIAL explicitly"
}

wait_for_boot() {
    local deadline=$((SECONDS + 180))
    while (( SECONDS < deadline )); do
        if [[ "$(adb_shell getprop sys.boot_completed 2>/dev/null | tr -d '\r')" == "1" ]]; then
            return 0
        fi
        sleep 2
    done
    fail "device ${selected_serial} did not finish booting"
}

ensure_tv_device() {
    local characteristics
    characteristics="$(adb_shell getprop ro.build.characteristics | tr -d '\r')"
    local product_name
    product_name="$(adb_shell getprop ro.product.name | tr -d '\r')"
    local product_model
    product_model="$(adb_shell getprop ro.product.model | tr -d '\r')"
    local build_flavor
    build_flavor="$(adb_shell getprop ro.build.flavor | tr -d '\r')"
    local avd_name
    avd_name="$(adb_shell getprop ro.boot.qemu.avd_name | tr -d '\r')"
    local ui_mode
    ui_mode="$(adb_shell dumpsys uimode | tr -d '\r')"

    if [[ "${characteristics}" == *tv* ]] ||
        [[ "${product_name}" == *atv* ]] ||
        [[ "${product_model}" == *atv* ]] ||
        [[ "${build_flavor}" == *atv* ]] ||
        [[ "${avd_name}" == *Television* ]] ||
        grep -Fq 'mCurUiMode=0x24' <<<"${ui_mode}"; then
        return 0
    fi

    fail "device ${selected_serial} is not an Android TV target (characteristics=${characteristics}, product=${product_name}, model=${product_model}, flavor=${build_flavor}, avd=${avd_name})"
}

ensure_device() {
    if selected_serial="$(choose_serial)"; then
        log "Using adb target ${selected_serial}"
        wait_for_boot
        ensure_tv_device
        return 0
    fi

    local avd="${XG2G_TV_SMOKE_AVD:-${DEFAULT_AVD}}"
    local emulator_bin
    emulator_bin="$(find_emulator_bin)" || fail "no adb device found and Android emulator binary is unavailable"
    log "Starting AVD ${avd} ..."
    "${emulator_bin}" @"${avd}" -no-snapshot-load >"${EMULATOR_LOG}" 2>&1 &
    started_emulator=1

    local deadline=$((SECONDS + 180))
    while (( SECONDS < deadline )); do
        if selected_serial="$(choose_serial 2>/dev/null)"; then
            log "Using adb target ${selected_serial}"
            wait_for_boot
            ensure_tv_device
            return 0
        fi
        sleep 2
    done

    fail "timed out waiting for AVD ${avd} to appear"
}

capture_ui() {
    local name="$1"
    adb_shell uiautomator dump "${UI_DUMP_PATH}" >/dev/null 2>&1 || true
    adb_cmd exec-out cat "${UI_DUMP_PATH}" > "${UI_DIR}/${name}.xml" || true
    adb_cmd exec-out screencap -p > "${SCREENSHOT_DIR}/${name}.png" || true
}

ui_contains() {
    local text="$1"
    local xml_file="$2"
    grep -Fq "${text}" "${xml_file}"
}

wait_for_ui_text() {
    local text="$1"
    local timeout_seconds="${2:-45}"
    local deadline=$((SECONDS + timeout_seconds))
    while (( SECONDS < deadline )); do
        capture_ui "wait-$(printf '%s' "${text}" | tr ' /' '__' | tr -cd '[:alnum:]_-')"
        local latest="${UI_DIR}/wait-$(printf '%s' "${text}" | tr ' /' '__' | tr -cd '[:alnum:]_-').xml"
        if [[ -f "${latest}" ]] && ui_contains "${text}" "${latest}"; then
            return 0
        fi
        sleep 1
    done
    fail "timed out waiting for UI text: ${text}"
}

wait_for_ui_any_text() {
    local timeout_seconds="$1"
    shift
    local deadline=$((SECONDS + timeout_seconds))
    while (( SECONDS < deadline )); do
        local text
        for text in "$@"; do
            local sanitized
            sanitized="$(printf '%s' "${text}" | tr ' /' '__' | tr -cd '[:alnum:]_-')"
            capture_ui "wait-${sanitized}"
            local latest="${UI_DIR}/wait-${sanitized}.xml"
            if [[ -f "${latest}" ]] && ui_contains "${text}" "${latest}"; then
                return 0
            fi
        done
        sleep 1
    done
    fail "timed out waiting for any UI text: $*"
}

assert_ui_not_contains() {
    local text="$1"
    local name="$2"
    capture_ui "${name}"
    local xml_file="${UI_DIR}/${name}.xml"
    if [[ -f "${xml_file}" ]] && ui_contains "${text}" "${xml_file}"; then
        fail "unexpected UI text present: ${text}"
    fi
}

tap_text() {
    local text="$1"
    local dump_file="${UI_DIR}/tap-target.xml"
    adb_shell uiautomator dump "${UI_DUMP_PATH}" >/dev/null 2>&1 || true
    adb_cmd exec-out cat "${UI_DUMP_PATH}" > "${dump_file}"

    local coords
    coords="$(python3 - "${dump_file}" "${text}" <<'PY'
import re
import sys
from xml.etree import ElementTree as ET

xml_path = sys.argv[1]
target = sys.argv[2]
tree = ET.parse(xml_path)
for node in tree.iter("node"):
    text = node.attrib.get("text", "")
    desc = node.attrib.get("content-desc", "")
    if text != target and desc != target:
        continue
    bounds = node.attrib.get("bounds", "")
    match = re.fullmatch(r"\[(\d+),(\d+)\]\[(\d+),(\d+)\]", bounds)
    if not match:
        continue
    left, top, right, bottom = map(int, match.groups())
    x = (left + right) // 2
    y = (top + bottom) // 2
    print(f"{x} {y}")
    sys.exit(0)
sys.exit(1)
PY
)" || fail "could not locate tappable UI node: ${text}"

    local x y
    read -r x y <<<"${coords}"
    adb_shell input tap "${x}" "${y}" >/dev/null
}

log_offset() {
    if [[ -f "${BACKEND_LOG}" ]]; then
        wc -c < "${BACKEND_LOG}" | tr -d ' '
    else
        printf '0\n'
    fi
}

wait_for_backend_regex() {
    local offset="$1"
    local regex="$2"
    local timeout_seconds="${3:-45}"
    local deadline=$((SECONDS + timeout_seconds))
    while (( SECONDS < deadline )); do
        if [[ -f "${BACKEND_LOG}" ]]; then
            if python3 - "${BACKEND_LOG}" "${offset}" "${regex}" <<'PY'
import pathlib
import re
import sys

log_path = pathlib.Path(sys.argv[1])
offset = int(sys.argv[2])
regex = sys.argv[3]
text = log_path.read_text(encoding="utf-8", errors="replace")
sys.exit(0 if re.search(regex, text[offset:]) else 1)
PY
            then
                return 0
            fi
        fi
        sleep 1
    done
    fail "timed out waiting for backend pattern: ${regex}"
}

curl_json() {
    local method="$1"
    local url="$2"
    local body="$3"
    local output_file="$4"
    local auth_token="${5:-}"

    local curl_args=(
        -sS
        -o "${output_file}"
        -w "%{http_code}"
        -X "${method}"
        -H "Content-Type: application/json"
        -H "Origin: ${LOCAL_HTTP_BASE}"
        -H "Referer: ${LOCAL_UI_REFERER}"
    )
    if [[ -n "${auth_token}" ]]; then
        curl_args+=(-H "Authorization: Bearer ${auth_token}")
    fi
    if [[ -n "${body}" ]]; then
        curl_args+=(-d "${body}")
    fi
    curl "${curl_args[@]}" "${url}"
}

json_field() {
    local json_file="$1"
    local field_name="$2"
    python3 - "${json_file}" "${field_name}" <<'PY'
import json
import sys

with open(sys.argv[1], encoding="utf-8") as handle:
    data = json.load(handle)
value = data.get(sys.argv[2])
if value is None:
    sys.exit(1)
if isinstance(value, bool):
    print("true" if value else "false")
else:
    print(value)
PY
}

start_backend() {
    if port_is_open "${TV_SMOKE_PORT}"; then
        fail "port ${TV_SMOKE_PORT} is already in use on 127.0.0.1; stop the existing process or rerun with XG2G_TV_SMOKE_PORT=<free-port>"
    fi

    rm -rf "${SMOKE_STORE_PATH}" "${SMOKE_HLS_ROOT}"
    mkdir -p "${SMOKE_STORE_PATH}" "${SMOKE_HLS_ROOT}"

    log "Building embedded WebUI bundle ..."
    make ui-build >/dev/null

    log "Building local backend binary ..."
    (
        cd "${ROOT}/backend"
        GOWORK=off go build -trimpath -buildvcs=false -mod=vendor -o "${BIN_PATH}" ./cmd/daemon
    )

    log "Starting local backend on ${LOCAL_HTTP_BASE} ..."
    (
        export XG2G_LISTEN=":${TV_SMOKE_PORT}"
        export XG2G_STORE_PATH="${SMOKE_STORE_PATH}"
        export XG2G_HLS_ROOT="${SMOKE_HLS_ROOT}"
        exec "${BIN_PATH}"
    ) >> "${BACKEND_LOG}" 2>&1 &
    backend_pid=$!

    local deadline=$((SECONDS + 30))
    while (( SECONDS < deadline )); do
        if curl -fsS --max-time 2 "${LOCAL_HTTP_BASE}/healthz" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    fail "local backend did not become ready"
}

install_app() {
    log "Installing Android dev build on ${selected_serial} ..."
    (
        cd "${ROOT}/android"
        ANDROID_SERIAL="${selected_serial}" ./gradlew :app:installDevDebug >/dev/null
    )
}

start_logcat() {
    adb_cmd logcat -c >/dev/null 2>&1 || true
    adb_cmd logcat > "${LOGCAT_LOG}" 2>&1 &
    logcat_pid=$!
}

pair_device() {
    local start_json="${LOG_DIR}/pairing-start.json"
    local approve_json="${LOG_DIR}/pairing-approve.json"
    local exchange_json="${LOG_DIR}/pairing-exchange.json"

    log "Creating paired device grant for TV smoke ..."
    local status
    status="$(curl_json "POST" "${LOCAL_HTTP_BASE}/api/v3/pairing/start" '{"deviceName":"TV Smoke","deviceType":"android_tv","requestedPolicyProfile":"tv-default"}' "${start_json}")"
    [[ "${status}" == "201" ]] || fail "pairing start failed with HTTP ${status}"

    local pairing_id pairing_secret
    pairing_id="$(json_field "${start_json}" "pairingId")" || fail "pairing start missing pairingId"
    pairing_secret="$(json_field "${start_json}" "pairingSecret")" || fail "pairing start missing pairingSecret"

    status="$(curl_json "POST" "${LOCAL_HTTP_BASE}/api/v3/pairing/${pairing_id}/approve" '{}' "${approve_json}" "${XG2G_API_TOKEN}")"
    if [[ "${status}" != "200" ]]; then
        fail "pairing approve failed with HTTP ${status}; local API token likely lacks v3:admin"
    fi

    status="$(curl_json "POST" "${LOCAL_HTTP_BASE}/api/v3/pairing/${pairing_id}/exchange" "{\"pairingSecret\":\"${pairing_secret}\"}" "${exchange_json}")"
    [[ "${status}" == "200" ]] || fail "pairing exchange failed with HTTP ${status}"

    DEVICE_GRANT_ID="$(json_field "${exchange_json}" "deviceGrantId")" || fail "exchange missing deviceGrantId"
    DEVICE_GRANT="$(json_field "${exchange_json}" "deviceGrant")" || fail "exchange missing deviceGrant"
}

launch_app() {
    log "Resetting dev app state ..."
    adb_shell pm clear "${PACKAGE_NAME}" >/dev/null
    adb_shell input keyevent KEYCODE_WAKEUP >/dev/null 2>&1 || true
    adb_shell input keyevent KEYCODE_MENU >/dev/null 2>&1 || true

    log "Launching paired TV app ..."
    adb_shell am force-stop "${PACKAGE_NAME}" >/dev/null 2>&1 || true
    adb_shell am start -W \
        -n "${MAIN_ACTIVITY}" \
        --es base_url "${EMULATOR_UI_BASE}" \
        --es device_grant_id "${DEVICE_GRANT_ID}" \
        --es device_grant "${DEVICE_GRANT}" >/dev/null
}

go_home_if_needed() {
    local attempt
    for attempt in 1 2 3; do
        capture_ui "home-check-${attempt}"
        local xml_file="${UI_DIR}/home-check-${attempt}.xml"
        if [[ -f "${xml_file}" ]] && ui_contains "Open web tools" "${xml_file}" && ui_contains "Watch Live TV" "${xml_file}"; then
            return 0
        fi
        adb_shell input keyevent KEYCODE_BACK >/dev/null 2>&1 || true
        sleep 1
    done
    fail "could not return to TV home screen"
}

run_guide_assertions() {
    local offset
    offset="$(log_offset)"
    tap_text "Watch Live TV"

    wait_for_backend_regex "${offset}" '"method":"POST","path":"/api/v3/auth/device/session".*"status":200' 45
    wait_for_backend_regex "${offset}" '"method":"POST","path":"/api/v3/auth/session".*"status":200' 45
    wait_for_backend_regex "${offset}" '"method":"GET","path":"/api/v3/services/bouquets".*"status":200' 45
    wait_for_backend_regex "${offset}" '"method":"GET","path":"/api/v3/services".*"status":200' 45
    wait_for_backend_regex "${offset}" '"method":"GET","path":"/api/v3/epg".*"status":200' 45

    wait_for_ui_any_text 45 "EPG ready" "EPG delayed"
    assert_ui_not_contains "Guide unavailable" "guide-ready"
    assert_ui_not_contains "Sign-in required" "guide-ready-auth"
    assert_ui_not_contains "No programme data" "guide-has-data"
}

run_playback_assertions() {
    local offset
    offset="$(log_offset)"
    adb_shell input keyevent KEYCODE_DPAD_CENTER >/dev/null

    wait_for_backend_regex "${offset}" '"method":"POST","path":"/api/v3/intents".*"status":202' 45
    wait_for_backend_regex "${offset}" '"method":"GET","path":"/api/v3/sessions/[^"]+".*"status":200' 45
    wait_for_backend_regex "${offset}" '"method":"GET","path":"/api/v3/sessions/[^"]+/hls/[^"]+".*"status":200' 45
    wait_for_backend_regex "${offset}" '"method":"POST","path":"/api/v3/sessions/[^"]+/heartbeat".*"status":200' 45
}

run_web_assertions() {
    go_home_if_needed
    local offset
    offset="$(log_offset)"
    tap_text "Open web tools"

    wait_for_backend_regex "${offset}" '"method":"POST","path":"/api/v3/auth/web-bootstrap".*"status":201' 45
    wait_for_backend_regex "${offset}" '"method":"GET","path":"/api/v3/system/config".*"status":200' 45
    assert_ui_not_contains "Authentication Required" "web-bootstrap-ready"
}

main() {
    require_cmd adb
    require_cmd curl
    require_cmd python3

    if [[ ! -f "${ENV_FILE}" ]]; then
        fail "missing ${ENV_FILE}"
    fi

    # shellcheck disable=SC1090
    set -a
    . "${ENV_FILE}"
    set +a

    [[ -n "${XG2G_API_TOKEN:-}" ]] || fail "XG2G_API_TOKEN must be set in ${ENV_FILE}"

    ensure_device
    start_backend
    install_app
    start_logcat
    pair_device
    launch_app

    wait_for_ui_text "Open web tools" 45
    wait_for_ui_text "Watch Live TV" 45

    run_guide_assertions
    run_playback_assertions
    run_web_assertions

    capture_ui "final"
}

main "$@"
