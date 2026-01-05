#!/usr/bin/env bash
set -euo pipefail

# Standardized Development Wrapper
# Uses .env and delegates to 'make dev' for consistent builds.

echo "🚀 Starting xg2g via 'make dev' (Loop Mode)..."

while true; do
    make dev >> logs/dev.log 2>&1
    echo "🔄 App exited. Restarting in 2 seconds..." >> logs/dev.log
    sleep 2
done
