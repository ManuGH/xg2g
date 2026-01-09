#!/usr/bin/env bash
# FFprobe wrapper - sets LD_LIBRARY_PATH scoped to FFprobe process only
set -euo pipefail

FFMPEG_HOME="${FFMPEG_HOME:-/opt/ffmpeg}"
FFPROBE_BIN="${FFPROBE_BIN:-${FFMPEG_HOME}/bin/ffprobe}"
FFMPEG_LIB="${FFMPEG_LIB:-${FFMPEG_HOME}/lib}"

# Validate FFprobe binary exists
if [ ! -x "${FFPROBE_BIN}" ]; then
    echo "ERROR: FFprobe binary not found or not executable: ${FFPROBE_BIN}" >&2
    echo "Set FFMPEG_HOME or FFPROBE_BIN to the correct location" >&2
    exit 1
fi

# Handle special --print-realpath flag for diagnostic tools
if [ "${1:-}" == "--print-realpath" ]; then
    echo "${FFPROBE_BIN}"
    exit 0
fi

# Scope LD_LIBRARY_PATH to this process only (no global leak)
export LD_LIBRARY_PATH="${FFMPEG_LIB}"

exec "${FFPROBE_BIN}" "$@"
