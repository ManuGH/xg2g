#!/usr/bin/env bash
set -euo pipefail

# Standardized Development Wrapper
# Uses .env and delegates to 'make dev' for consistent builds.

echo "🚀 Starting xg2g via 'make dev' (Loop Mode)..."

while true; do
    make dev
    echo "🔄 App exited. Restarting in 2 seconds..."
    sleep 2
done
