#!/usr/bin/env bash
set -Eeuo pipefail

# Required:
#   STREAM_URL=http://receiver:8001/<service-reference>
#
# Optional:
#   NAME=orf1
#   DURATION=90
#   OUT_ROOT=./testdata/regression-corpus
#   STREAM_USER=user
#   STREAM_PASS=password

: "${STREAM_URL:?STREAM_URL must point to the raw Enigma2 MPEG-TS stream}"

NAME="${NAME:-orf1}"
DURATION="${DURATION:-90}"
OUT_ROOT="${OUT_ROOT:-./testdata/regression-corpus}"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT_DIR="${OUT_ROOT}/${NAME}/${STAMP}"
RAW_FILE="${OUT_DIR}/input.ts"

mkdir -p "${OUT_DIR}"

auth_args=()
if [[ -n "${STREAM_USER:-}" ]]; then
    auth_args+=(--user "${STREAM_USER}:${STREAM_PASS:-}")
fi

echo "Capturing ${NAME} for ${DURATION}s"
echo "Output: ${OUT_DIR}"

start_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
start_epoch="$(date +%s.%N)"
capture_pid=""
termination_mode="completed"
capture_rc=0

cleanup() {
    rc=$?
    end_time="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
    end_epoch="$(date +%s.%N)"
    actual_duration=$(awk "BEGIN {print $end_epoch - $start_epoch}")
    
    if [[ -n "${capture_pid}" ]]; then
        kill "${capture_pid}" 2>/dev/null || true
        wait "${capture_pid}" 2>/dev/null || true
    fi

    if [[ ! -s "${RAW_FILE}" ]]; then
        echo "Capture produced an empty file or was not started" >&2
        exit 1
    fi

    bytes="$(wc -c < "${RAW_FILE}" | tr -d ' ')"
    packets="$((bytes / 188))"
    remainder="$((bytes % 188))"

    if command -v sha256sum >/dev/null 2>&1; then
        digest="$(sha256sum "${RAW_FILE}" | awk '{print $1}')"
    else
        digest="$(shasum -a 256 "${RAW_FILE}" | awk '{print $1}')"
    fi

    printf '%s  input.ts
' "${digest}" > "${OUT_DIR}/SHA256SUMS"

    export NAME STAMP DURATION STREAM_URL
    export CAPTURE_RC="${capture_rc}"
    export CAPTURE_BYTES="${bytes}"
    export TS_PACKETS="${packets}"
    export TS_REMAINDER="${remainder}"
    export SHA256="${digest}"
    export MANIFEST_PATH="${OUT_DIR}/manifest.json"
    export START_TIME="${start_time}"
    export END_TIME="${end_time}"
    export ACTUAL_DURATION="${actual_duration}"
    export TERM_MODE="${termination_mode}"

    python3 <<'PY'
import json
import os
from urllib.parse import urlsplit, urlunsplit

url = urlsplit(os.environ["STREAM_URL"])
host = url.hostname or ""
if url.port:
    host = f"{host}:{url.port}"

safe_url = urlunsplit((url.scheme, host, url.path, "", ""))
duration = float(os.environ["DURATION"])
actual = float(os.environ["ACTUAL_DURATION"])

manifest = {
    "schema_version": 1,
    "fixture_name": os.environ["NAME"],
    "captured_at_utc": os.environ["STAMP"],
    "started_at": os.environ["START_TIME"],
    "ended_at": os.environ["END_TIME"],
    "requested_duration_seconds": duration,
    "actual_duration_seconds": round(actual, 3),
    "completed_requested_duration": (os.environ["TERM_MODE"] == "completed" and actual >= duration - 2),
    "source_url_redacted": safe_url,
    "capture": {
        "tool": "curl",
        "termination": {
            "mode": os.environ["TERM_MODE"],
            "signal": "SIGTERM" if os.environ["TERM_MODE"] == "manual_stop" else None,
            "exit_code": int(os.environ["CAPTURE_RC"]) if os.environ["CAPTURE_RC"] else None
        },
        "bytes": int(os.environ["CAPTURE_BYTES"]),
        "complete_ts_packets": int(os.environ["TS_PACKETS"]),
        "trailing_bytes": int(os.environ["TS_REMAINDER"]),
        "sha256": os.environ["SHA256"],
        "transformed": False,
    },
}

with open(os.environ["MANIFEST_PATH"], "w", encoding="utf-8") as file:
    json.dump(manifest, file, indent=2)
    file.write("
")
PY

    if command -v ffprobe >/dev/null 2>&1; then
        ffprobe -hide_banner -v error -show_format -show_programs -show_streams -of json "${RAW_FILE}" > "${OUT_DIR}/ffprobe.json" || true
        ffprobe -hide_banner -v error -show_packets -show_entries packet=stream_index,pts,dts,pts_time,dts_time,duration_time,flags,pos,size -of csv=p=0 "${RAW_FILE}" 2> "${OUT_DIR}/ffprobe-packets.stderr" | gzip -9 > "${OUT_DIR}/packets.csv.gz" || true
    fi

    if command -v tsanalyze >/dev/null 2>&1; then
        tsanalyze --deterministic --json "${RAW_FILE}" > "${OUT_DIR}/tsduck.json" 2> "${OUT_DIR}/tsduck-json.stderr" || true
        tsanalyze --deterministic --error-analysis "${RAW_FILE}" > "${OUT_DIR}/tsduck-errors.txt" 2>&1 || true
    fi

    if command -v tsp >/dev/null 2>&1; then
        tsp -I file "${RAW_FILE}" -P continuity --json-line=CONTINUITY -O drop > "${OUT_DIR}/continuity.log" 2>&1 || true
    fi

    cat > "${OUT_DIR}/notes.md" <<NOTEEOF
# Capture Notes

- Channel: ${NAME}
- Captured UTC: ${STAMP}
- Requested duration: ${DURATION} seconds
- Actual duration: ${actual_duration} seconds
- Bytes: ${bytes}

## Reproduction context

- xg2g version/commit:
- Receiver model:
- Receiver firmware:
- Client:
- Browser/OS:
- Selected xg2g profile:
- Observed symptom:
- Approximate failure timestamp:
- Audio track:
- Additional notes:
NOTEEOF

    echo
    echo "Capture complete (${termination_mode})"
    echo "SHA-256: ${digest}"
    echo "Directory: ${OUT_DIR}"
    exit $rc
}

on_interrupt() {
    termination_mode="manual_stop"
    exit 130
}

trap cleanup EXIT
trap on_interrupt INT TERM

# curl exit code 28 is expected when --max-time ends a live stream.
set +e
curl     --fail     --location     --silent     --show-error     --no-buffer     --connect-timeout 10     --max-time "${DURATION}"     "${auth_args[@]}"     --output "${RAW_FILE}"     "${STREAM_URL}" &
capture_pid=$!
wait $capture_pid
capture_rc=$?
set -e

if [[ ${capture_rc} -ne 0 && ${capture_rc} -ne 28 && ${termination_mode} != "manual_stop" ]]; then
    echo "Capture failed with curl exit code ${capture_rc}" >&2
    exit "${capture_rc}"
fi
