#!/usr/bin/env bash
set -euo pipefail

# Standardized Development Wrapper
# Uses .env and delegates to 'make backend-dev' for consistent builds.

echo "🚀 Starting xg2g via 'make backend-dev' (Loop Mode)..."
export PATH="/opt/ffmpeg/bin:$PATH"
export LD_LIBRARY_PATH="/opt/ffmpeg/lib:${LD_LIBRARY_PATH:-}"

mkdir -p logs

while true; do
    make backend-dev >> logs/dev.log 2>&1
    echo "🔄 App exited. Restarting in 2 seconds..." >> logs/dev.log
    sleep 2
done
