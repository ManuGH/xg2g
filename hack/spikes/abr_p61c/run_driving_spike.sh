#!/usr/bin/env bash
set -euo pipefail

# Master Driving Profile Runner for P6.1c ABR Spike
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
OUT_DIR="/tmp/xg2g_abr_p61c"
PORT="8899"

echo "======================================================="
echo "   xg2g P6.1c 3-Tier HLS Driving Profile Orchestrator  "
echo "======================================================="

# Stop any previous spike processes
pkill -9 -f "ffmpeg_3tier_rendition" || true
pkill -9 -f "ffmpeg" || true
pkill -9 -f "server.go" || true
lsof -ti :8899 | xargs kill -9 2>/dev/null || true
sleep 1

rm -rf "${OUT_DIR}" 2>/dev/null || true
mkdir -p "${OUT_DIR}"

# Step 1: Start FFmpeg 3-Tier Rendition process
echo "[1/4] Launching FFmpeg 3-Tier Dual/Triple Rendition Stream..."
"${SCRIPT_DIR}/ffmpeg_3tier_rendition.sh" "${OUT_DIR}"
sleep 5

# Step 2: Run automated pre-flight manifest and PTS sanity check
echo "[2/4] Running Automated Pre-Flight Manifest & PTS Sanity Check..."
go run "${SCRIPT_DIR}/check_manifest_sanity.go" "${OUT_DIR}"

# Step 3: Start HTTP Server
echo "[3/4] Starting HTTP Spike Server on http://localhost:${PORT}..."
go run "${SCRIPT_DIR}/server.go" "${OUT_DIR}" "${PORT}" &
SERVER_PID=$!
sleep 2

# Step 4: Run Automated Driving Profile Playwright Test
echo "[4/4] Running 6-Stage Driving Profile Playwright Telemetry Test..."
node "${SCRIPT_DIR}/test_driving_profile.js"

# Cleanup background server process
kill -9 "${SERVER_PID}" 2>/dev/null || true
