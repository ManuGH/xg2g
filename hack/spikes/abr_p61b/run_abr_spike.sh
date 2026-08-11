#!/usr/bin/env bash
set -euo pipefail

# Master Test Runner for P6.1b ABR Spike
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="/tmp/xg2g_abr_p61b"
PORT="8899"

echo "======================================================="
echo "   xg2g P6.1b Dual-Rendition HLS Spike Orchestrator    "
echo "======================================================="

# Stop any previous spike processes aggressively
pkill -9 -f "ffmpeg_dual_rendition" || true
pkill -9 -f "ffmpeg" || true
pkill -9 -f "server.go" || true
lsof -ti :8899 | xargs kill -9 2>/dev/null || true
sleep 1

rm -rf "${OUT_DIR}" 2>/dev/null || true
mkdir -p "${OUT_DIR}"

# Step 1: Start FFmpeg Dual-Rendition process
echo "[1/4] Launching FFmpeg Dual-Rendition Stream..."
"${SCRIPT_DIR}/ffmpeg_dual_rendition.sh" "${OUT_DIR}"
sleep 5

# Step 2: Run automated pre-flight manifest sanity check
echo "[2/4] Running Automated Pre-Flight Manifest Sanity Check..."
go run "${SCRIPT_DIR}/check_manifest_sanity.go" "${OUT_DIR}"

# Step 3: Start HTTP Server
echo "[3/4] Starting HTTP Spike Server on http://localhost:${PORT}..."
go run "${SCRIPT_DIR}/server.go" "${OUT_DIR}" "${PORT}" &
SERVER_PID=$!
sleep 2

# Step 4: Run Automated Playwright ABR Switch Test
echo "[4/4] Running Automated Playwright ABR Telemetry Test..."
node "${SCRIPT_DIR}/test_abr_browser.js"

# Cleanup background server process
kill -9 "${SERVER_PID}" 2>/dev/null || true
