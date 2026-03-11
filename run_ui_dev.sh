#!/usr/bin/env bash
set -euo pipefail

mkdir -p logs

cleanup() {
    if [[ -n "${vite_pid:-}" ]]; then
        kill "${vite_pid}" 2>/dev/null || true
        wait "${vite_pid}" 2>/dev/null || true
    fi
}

trap cleanup EXIT INT TERM

echo "Starting Vite dev server in the background..."
(
    cd frontend/webui
    if [[ ! -d node_modules ]]; then
        npm ci
    fi
    npm run dev >> ../../logs/webui-dev.log 2>&1
) &
vite_pid=$!

echo "Vite logs: logs/webui-dev.log"
echo "Starting backend dev server with -tags=dev on http://localhost:8080/ui/ ..."

make backend-dev-ui
