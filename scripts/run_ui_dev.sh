#!/usr/bin/env bash
set -euo pipefail

mkdir -p logs

# shellcheck disable=SC2317 # Invoked through the EXIT trap.
cleanup() {
    local pid
    for pid in "${vite_pid:-}" "${backend_pid:-}"; do
        [[ -n "${pid}" ]] || continue
        kill "${pid}" 2>/dev/null || true
        wait "${pid}" 2>/dev/null || true
    done
}

trap cleanup EXIT
trap 'exit 130' INT TERM

[[ -d frontend/webui/node_modules ]] || {
    echo "Missing frontend/webui/node_modules. Run 'make install' before 'make dev-ui'." >&2
    exit 1
}

echo "Starting Vite dev server in the background..."
(
    cd frontend/webui
    npm run dev >> ../../logs/webui-dev.log 2>&1
) &
vite_pid=$!

echo "Vite logs: logs/webui-dev.log"
echo "Starting backend dev server with -tags=dev on http://localhost:8080/ui/ ..."

make backend-dev-ui &
backend_pid=$!

# Portable wait-any loop compatible with older Bash versions (e.g., macOS default Bash 3.2)
while kill -0 "${vite_pid}" 2>/dev/null && kill -0 "${backend_pid}" 2>/dev/null; do
    sleep 1
done

if ! kill -0 "${vite_pid}" 2>/dev/null; then
    wait "${vite_pid}"
    rc=$?
else
    wait "${backend_pid}"
    rc=$?
fi

if [[ "${rc}" -eq 0 ]]; then
    echo "A development process exited unexpectedly." >&2
    exit 1
fi
exit "${rc}"
