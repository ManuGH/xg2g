#!/usr/bin/env bash
# FFmpeg wrapper - sets LD_LIBRARY_PATH scoped to FFmpeg process only
# FIX: Point to the actual binary location in this environment
set -euo pipefail

FFMPEG_HOME="${FFMPEG_HOME:-/opt/ffmpeg}"
FFMPEG_BIN="${FFMPEG_BIN:-${FFMPEG_HOME}/bin/ffmpeg}"
FFMPEG_LIB="${FFMPEG_LIB:-${FFMPEG_HOME}/lib}"

# Handle special --print-realpath flag for diagnostic tools
if [ "${1:-}" == "--print-realpath" ]; then
    echo "${FFMPEG_BIN}"
    exit 0
fi

# Validate FFmpeg binary exists
if [ ! -x "${FFMPEG_BIN}" ]; then
    echo "ERROR: FFmpeg binary not found or not executable: ${FFMPEG_BIN}" >&2
    echo "Set FFMPEG_HOME or FFMPEG_BIN to the correct location" >&2
    exit 1
fi

# Scope LD_LIBRARY_PATH to this process only (no global leak)
export LD_LIBRARY_PATH="${FFMPEG_LIB}"

exec "${FFMPEG_BIN}" "$@"
