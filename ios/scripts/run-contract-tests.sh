#!/usr/bin/env bash
#
# Runs the iOS client against a real xg2g backend.
#
# Everything else in the iOS suite talks to Swift doubles. This is the only
# place where the actual client stack — Secure Enclave key, DPoP proofs,
# coordinators, URLSession — meets the production Go router and real SQLite.
# Both contract defects found so far (the stale ExchangePairingResponse schema
# and the missing Apple device types) were of the kind that doubles reproduce
# faithfully on both sides.
#
# Usage: ios/scripts/run-contract-tests.sh [simulator name]

set -euo pipefail

SIMULATOR="${1:-iPhone 17 Pro}"
# Fixed, and matched by the address in Xg2g.xcscheme. A configurable port here
# would silently point the fixture and the client at different places.
PORT=18422
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BASE_URL="http://127.0.0.1:${PORT}"

# Kill the whole process group, not the subshell.
#
# `kill $!` reaches only the subshell and leaves `go test` — and the listening
# server — orphaned. An orphan then holds the port and answers the next run
# from a stale database until its own timeout expires, which is precisely the
# kind of ghost that makes an integration suite fail for reasons that have
# nothing to do with the code.
cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill -TERM -- "-${SERVER_PID}" 2>/dev/null || kill -TERM "${SERVER_PID}" 2>/dev/null || true
    wait "${SERVER_PID}" 2>/dev/null || true
  fi
  # Belt and braces: anything still holding the fixture port is ours.
  local holder
  holder="$(lsof -ti "tcp:${PORT}" 2>/dev/null || true)"
  if [[ -n "${holder}" ]]; then
    echo "${holder}" | xargs kill -9 2>/dev/null || true
  fi
}
trap cleanup EXIT

# A leftover fixture would answer with a stale database and make this run
# meaningless, so refuse rather than inherit it.
if lsof -ti "tcp:${PORT}" >/dev/null 2>&1; then
  echo "!! port ${PORT} is already in use; a previous fixture is still running" >&2
  exit 1
fi

echo "==> starting contract fixture on ${BASE_URL}"
set -m
(
  cd "${REPO_ROOT}/backend"
  XG2G_IOS_CONTRACT_PORT="${PORT}" XG2G_IOS_CONTRACT_TTL_SECONDS="900" \
    go test ./internal/control/http/v3/ -run TestIOSContractServer -count=1 -timeout 20m -v
) &
SERVER_PID=$!
set +m

# Wait for the port rather than sleeping a guessed interval: a fixed sleep is
# how this kind of script becomes the flakiest thing in the repository.
for _ in $(seq 1 100); do
  if nc -z 127.0.0.1 "${PORT}" 2>/dev/null; then
    READY=1
    break
  fi
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    echo "!! contract fixture exited before it was listening" >&2
    exit 1
  fi
  sleep 0.2
done

if [[ -z "${READY:-}" ]]; then
  echo "!! contract fixture never listened on ${PORT}" >&2
  exit 1
fi

# Guarantee that the entire test target compiles cleanly before running.
# -only-testing must never hide compilation errors in other test suites.
"${REPO_ROOT}/ios/scripts/verify-ios-build.sh" "${SIMULATOR}"

echo "==> running iOS contract suite against ${BASE_URL}"
cd "${REPO_ROOT}/ios"

# The address lives in the scheme's test environment, not on this command line:
# xcodebuild takes build settings, but a scheme environment variable does not
# expand them. The suite enables itself by finding the fixture listening.
OUTPUT="$(mktemp)"
set +e
xcodebuild test \
  -project Xg2g.xcodeproj \
  -scheme Xg2g \
  -destination "platform=iOS Simulator,name=${SIMULATOR}" \
  -only-testing:Xg2gTests/BackendContractTests \
  2>&1 | tee "${OUTPUT}"
STATUS="${PIPESTATUS[0]}"
set -e

if [[ "${STATUS}" -ne 0 ]]; then
  echo "!! contract suite failed" >&2
  exit "${STATUS}"
fi

# xcodebuild reports a fully skipped suite as TEST SUCCEEDED. That is precisely
# how a contract test comes to mean nothing while looking green, so the skip is
# treated as a failure of this script rather than a result.
if grep -q "Suite BackendContractTests skipped" "${OUTPUT}"; then
  echo "!! the contract suite was SKIPPED - it never reached the server." >&2
  echo "   XG2G_CONTRACT_BASE_URL did not arrive in the test runner." >&2
  exit 1
fi

if ! grep -qE "Suite BackendContractTests passed" "${OUTPUT}"; then
  echo "!! the contract suite did not report a pass; refusing to call this green." >&2
  exit 1
fi

echo "==> contract suite ran against the real backend and passed"
