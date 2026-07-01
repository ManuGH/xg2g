#!/usr/bin/env bash
set -euo pipefail

# Safe Shutdown Script
# Targets only local xg2g development processes.

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
backend_pattern="^${REPO_ROOT}/bin/xg2g(-dev)?([[:space:]]|$)"

echo "Initiating safe process termination..."

# 1. Stop the dev loop first
if pgrep -f "[r]un_dev.sh" > /dev/null; then
    echo "Stopping run_dev.sh..."
    pkill -f "[r]un_dev.sh"
fi

# 2. Stop only a backend binary launched from this development workspace.
if pgrep -f "${backend_pattern}" > /dev/null; then
    echo "Stopping xg2g development backend..."
    pkill -f "${backend_pattern}"
fi

# 3. Stop only the explicitly named local development container.
if command -v docker >/dev/null 2>&1; then
    dev_container="$(docker ps -q --filter 'name=^/xg2g-dev$')"
    if [[ -n "${dev_container}" ]]; then
        echo "Stopping xg2g development container..."
        docker stop "${dev_container}" > /dev/null
    fi
fi

echo "Safe shutdown complete."
