#!/usr/bin/env bash
# Hermetic behavioral contract for release-verify-remote.sh.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
VERIFY_REMOTE="${SCRIPT_DIR}/release-verify-remote.sh"
TMP_ROOT="$(mktemp -d)"
SERVER_SCRIPT="${TMP_ROOT}/registry_fixture.py"
SERVER_LOG="${TMP_ROOT}/registry.log"
PORT_FILE="${TMP_ROOT}/port"

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]]; then
    kill "${SERVER_PID}" >/dev/null 2>&1 || true
    wait "${SERVER_PID}" >/dev/null 2>&1 || true
  fi
  rm -rf "${TMP_ROOT}"
}
trap cleanup EXIT

cat > "${SERVER_SCRIPT}" <<'PY'
import hashlib
import json
import sys
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from socketserver import TCPServer

INDEX = {
    "schemaVersion": 2,
    "mediaType": "application/vnd.oci.image.index.v1+json",
    "manifests": [
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "digest": "sha256:" + "1" * 64,
            "platform": {"os": "linux", "architecture": "amd64"},
        },
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "digest": "sha256:" + "2" * 64,
            "platform": {"os": "linux", "architecture": "arm64"},
        },
        {
            "mediaType": "application/vnd.oci.image.manifest.v1+json",
            "digest": "sha256:" + "3" * 64,
            "platform": {"os": "unknown", "architecture": "unknown"},
        },
    ],
}
BODY = json.dumps(INDEX, separators=(",", ":")).encode()
DIGEST = "sha256:" + hashlib.sha256(BODY).hexdigest()
MISMATCH_DIGEST = "sha256:" + "f" * 64


class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path.startswith("/token"):
            payload = json.dumps({"token": "fixture-token"}).encode()
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(payload)))
            self.end_headers()
            self.wfile.write(payload)
            return

        prefix = "/v2/manugh/xg2g/manifests/"
        if self.path.startswith(prefix):
            reference = self.path[len(prefix) :]
            if reference not in {"v9.9.9", "latest", "mismatch"}:
                self.send_error(404)
                return
            self.send_response(200)
            self.send_header("Content-Type", INDEX["mediaType"])
            self.send_header(
                "Docker-Content-Digest",
                MISMATCH_DIGEST if reference == "mismatch" else DIGEST,
            )
            self.send_header("Content-Length", str(len(BODY)))
            self.end_headers()
            self.wfile.write(BODY)
            return

        self.send_error(404)

    def log_message(self, fmt, *args):
        return


class FixtureServer(ThreadingHTTPServer):
    def server_bind(self):
        # HTTPServer performs a reverse-DNS lookup during bind. Avoid that
        # non-hermetic network dependency in this local fixture.
        TCPServer.server_bind(self)
        self.server_name = "localhost"
        self.server_port = self.server_address[1]


server = FixtureServer(("127.0.0.1", 0), Handler)
with open(sys.argv[1], "w", encoding="utf-8") as port_file:
    port_file.write(str(server.server_address[1]))
server.serve_forever()
PY

python3 "${SERVER_SCRIPT}" "${PORT_FILE}" >"${SERVER_LOG}" 2>&1 &
SERVER_PID=$!

for _ in $(seq 1 50); do
  [[ -s "${PORT_FILE}" ]] && break
  sleep 0.1
done
[[ -s "${PORT_FILE}" ]] || {
  ps -p "${SERVER_PID}" -o pid=,stat=,command= >&2 || true
  cat "${SERVER_LOG}" >&2
  echo "fixture registry failed to start" >&2
  exit 1
}

PORT="$(cat "${PORT_FILE}")"
export XG2G_REGISTRY_API_BASE="http://127.0.0.1:${PORT}"
export XG2G_REGISTRY_TOKEN_URL="${XG2G_REGISTRY_API_BASE}/token"

RESULT="$(
  "${VERIFY_REMOTE}" \
    --version v9.9.9 \
    --also-tag latest \
    --require-platform linux/amd64 \
    --require-platform linux/arm64 \
    --json
)"

jq -e '
  .image == "ghcr.io/manugh/xg2g" and
  .tag == "v9.9.9" and
  (.digest | test("^sha256:[a-f0-9]{64}$")) and
  .platforms == ["linux/amd64", "linux/arm64"] and
  .equivalent_tag == "latest"
' <<<"${RESULT}" >/dev/null

if "${VERIFY_REMOTE}" --version v9.9.9 --require-platform linux/s390x >/dev/null 2>&1; then
  echo "missing-platform guard accepted an invalid OCI index" >&2
  exit 1
fi

if "${VERIFY_REMOTE}" --version v9.9.9 --also-tag mismatch >/dev/null 2>&1; then
  echo "equivalent-tag guard accepted different OCI digests" >&2
  exit 1
fi

echo "OK: remote release verifier contract holds."
