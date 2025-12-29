#!/usr/bin/env bash
set -euo pipefail

# Standardized Development Wrapper
# delegates to 'make dev' to ensure consistent builds (UI + Backend).

# Environment Configuration (migrated from legacy script)
export XG2G_DATA="${XG2G_DATA:-/data}"
export XG2G_V3_E2_HOST="${XG2G_V3_E2_HOST:-http://10.10.55.64}"
export XG2G_API_TOKEN="${XG2G_API_TOKEN:-dev-token}"
export XG2G_BOUQUET="${XG2G_BOUQUET:-Premium}"
export XG2G_V3_DVR_WINDOW="${XG2G_V3_DVR_WINDOW:-2700}"
export XG2G_DEV="${XG2G_DEV:-true}"
export XG2G_INITIAL_REFRESH="${XG2G_INITIAL_REFRESH:-false}"
export XG2G_V3_HLS_ROOT="${XG2G_V3_HLS_ROOT:-/data/v3-hls}"
export XG2G_V3_STORE_PATH="${XG2G_V3_STORE_PATH:-/data/v3-store}"
export XG2G_LOG_LEVEL="${XG2G_LOG_LEVEL:-debug}"
export XG2G_USE_RUST_REMUXER="${XG2G_USE_RUST_REMUXER:-true}"
export XG2G_LISTEN="${XG2G_LISTEN:-:8088}"
export XG2G_V3_WORKER_MODE="${XG2G_V3_WORKER_MODE:-standard}"
export XG2G_V3_IDLE_TIMEOUT="${XG2G_V3_IDLE_TIMEOUT:-30s}"
# Add other variables as needed from .env or override here

echo "🚀 Starting xg2g via 'make dev'..."
echo "   (This ensures WebUI is built and embedded correctly)"
exec make dev
