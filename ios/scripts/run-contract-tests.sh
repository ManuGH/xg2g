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
  if [[ -n "${FIXTURE_LOG:-}" ]]; then
    rm -f "${FIXTURE_LOG}"
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

# Compile before timing anything.
#
# The wait below measures how long the fixture takes to open a port. On a warm
# machine that is the only thing it measures; on a cold CI runner, `go test`
# first downloads modules and links the whole v3 package tree, which took longer
# than the entire wait budget and reported itself as "never listened" — a
# startup failure for what was really a build still in progress.
#
# Running the same package with a test filter that matches nothing does that
# work up front, through `go test` so the build flags stay identical.
# Guarantee that the entire test target compiles cleanly before anything else.
# -only-testing must never hide compilation errors in other test suites, and a
# Swift break has no reason to wait for a Go build and a fixture server first -
# it cannot be caused by either, and it fails the job regardless.
"${REPO_ROOT}/ios/scripts/verify-ios-build.sh" "${SIMULATOR}"

echo "==> building the contract fixture"
(
  cd "${REPO_ROOT}/backend"
  go test ./internal/control/http/v3/ -run '^$' -count=1 >/dev/null
)

FIXTURE_LOG="$(mktemp)"
echo "==> starting contract fixture on ${BASE_URL}"
set -m
(
  cd "${REPO_ROOT}/backend"
  XG2G_IOS_CONTRACT_PORT="${PORT}" XG2G_IOS_CONTRACT_TTL_SECONDS="900" \
    go test ./internal/control/http/v3/ -run TestIOSContractServer -count=1 -timeout 20m -v
) >"${FIXTURE_LOG}" 2>&1 &
SERVER_PID=$!
set +m

# Whatever the fixture said is the only evidence of why it did not come up, so
# it is printed on every failure path below rather than left in a temp file.
report_fixture_log() {
  echo "---- fixture output ----" >&2
  tail -40 "${FIXTURE_LOG}" >&2 || true
  echo "------------------------" >&2
}

# Wait for the port rather than sleeping a guessed interval: a fixed sleep is
# how this kind of script becomes the flakiest thing in the repository.
# 60s rather than 20s. The loop exits the moment the port answers, so this is an
# upper bound and not a delay; it only has to exceed process start on the
# slowest machine that runs this.
for _ in $(seq 1 300); do
  if nc -z 127.0.0.1 "${PORT}" 2>/dev/null; then
    READY=1
    break
  fi
  if ! kill -0 "${SERVER_PID}" 2>/dev/null; then
    echo "!! contract fixture exited before it was listening" >&2
    report_fixture_log
    exit 1
  fi
  sleep 0.2
done

if [[ -z "${READY:-}" ]]; then
  echo "!! contract fixture never listened on ${PORT}" >&2
  report_fixture_log
  exit 1
fi

if [[ "${SIMULATOR}" =~ ^[0-9A-Fa-f-]+$ ]]; then
  DESTINATION="platform=iOS Simulator,id=${SIMULATOR}"
  echo "==> Ensuring simulator ${SIMULATOR} is fully booted..."
  xcrun simctl bootstatus "${SIMULATOR}" -b
else
  DESTINATION="platform=iOS Simulator,name=${SIMULATOR}"
fi

echo "==> running iOS contract suite against ${BASE_URL} on ${DESTINATION}"
cd "${REPO_ROOT}/ios"

# The address lives in the scheme's test environment, not on this command line:
# xcodebuild takes build settings, but a scheme environment variable does not
# expand them. The suite enables itself by finding the fixture listening.
OUTPUT="$(mktemp)"
set +e
xcodebuild test \
  -project Xg2g.xcodeproj \
  -scheme Xg2g \
  -destination "${DESTINATION}" \
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
