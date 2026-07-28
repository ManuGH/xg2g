#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
SETUP="${REPO_ROOT}/infra/systemd/setup-linux.sh"
TEMP_ROOT="$(mktemp -d)"
trap 'rm -rf "${TEMP_ROOT}"' EXIT

fail() {
  printf 'ERROR: %s\n' "$*" >&2
  exit 1
}

bash -n "${REPO_ROOT}/infra/systemd/xg2g-admin.sh"
bash -n "${REPO_ROOT}/infra/systemd/setup-linux.sh"

XG2G_SETUP_E2_HOST="http://192.0.2.20" \
XG2G_SETUP_ACCESS_MODE="local" \
XG2G_SETUP_DVR_WINDOW="1h" \
XG2G_SETUP_CONCURRENT_STREAMS="2" \
XG2G_SETUP_STORAGE_MODE="shared" \
XG2G_SETUP_GPU_MODE="none" \
XG2G_SETUP_API_TOKEN="1111111111111111111111111111111111111111111111111111111111111111" \
XG2G_SETUP_DECISION_SECRET="2222222222222222222222222222222222222222222222222222222222222222" \
  "${SETUP}" --non-interactive --no-start --ref HEAD --source-dir "${REPO_ROOT}" --install-root "${TEMP_ROOT}" >/dev/null

ADMIN="${TEMP_ROOT}/srv/xg2g/scripts/xg2g-admin.sh"
[[ -x "${ADMIN}" ]] || fail "admin helper was not installed"
[[ -x "${TEMP_ROOT}/usr/local/sbin/xg2g-admin" ]] ||
  fail "stable xg2g-admin command was not installed"
[[ -f "${TEMP_ROOT}/etc/systemd/system/xg2g-backup.timer" ]] ||
  fail "backup timer was not installed"
[[ -f "${TEMP_ROOT}/srv/xg2g/INSTALL_REF" ]] ||
  fail "installed ref provenance was not recorded"

python3 - "${TEMP_ROOT}/var/lib/xg2g/sessions.sqlite" <<'PY'
import sqlite3
import sys

with sqlite3.connect(sys.argv[1]) as db:
    db.execute("create table sample(value text)")
    db.execute("insert into sample values ('before')")
PY
printf '{"channels":["one"]}\n' > "${TEMP_ROOT}/var/lib/xg2g/channels.json"

"${ADMIN}" doctor --install-root "${TEMP_ROOT}" >/dev/null
archive="$("${ADMIN}" backup --install-root "${TEMP_ROOT}" | tail -n 1)"
[[ -f "${archive}" ]] || fail "backup archive was not created"
[[ "$(stat -c '%a' "${archive}" 2>/dev/null || stat -f '%Lp' "${archive}")" == "600" ]] ||
  fail "backup archive must be mode 0600"

python3 - "${TEMP_ROOT}/var/lib/xg2g/sessions.sqlite" <<'PY'
import sqlite3
import sys

with sqlite3.connect(sys.argv[1]) as db:
    db.execute("update sample set value='after'")
PY
"${ADMIN}" restore "${archive}" --yes --install-root "${TEMP_ROOT}" >/dev/null
restored="$(python3 - "${TEMP_ROOT}/var/lib/xg2g/sessions.sqlite" <<'PY'
import sqlite3
import sys
with sqlite3.connect(sys.argv[1]) as db:
    print(db.execute("select value from sample").fetchone()[0])
PY
)"
[[ "${restored}" == "before" ]] || fail "restore did not recover SQLite state"

malicious_archive="${TEMP_ROOT}/malicious-link.tar.gz"
python3 - "${malicious_archive}" "${TEMP_ROOT}/etc/xg2g/xg2g.env" <<'PY'
import hashlib
import io
import json
import tarfile
import sys
from pathlib import Path

archive = Path(sys.argv[1])
env_path = Path(sys.argv[2])
env_data = env_path.read_bytes()
manifest = {
    "format": 1,
    "files": {
        "xg2g.env": {"sha256": hashlib.sha256(env_data).hexdigest(), "size": len(env_data)},
        "state/channels.json": {
            "sha256": hashlib.sha256(env_data).hexdigest(),
            "size": len(env_data),
        },
    },
}
with tarfile.open(archive, "w:gz") as bundle:
    env_member = tarfile.TarInfo("xg2g.env")
    env_member.size = len(env_data)
    bundle.addfile(env_member, io.BytesIO(env_data))
    link = tarfile.TarInfo("state/channels.json")
    link.type = tarfile.SYMTYPE
    link.linkname = str(env_path)
    bundle.addfile(link)
    manifest_data = (json.dumps(manifest) + "\n").encode()
    manifest_member = tarfile.TarInfo("manifest.json")
    manifest_member.size = len(manifest_data)
    bundle.addfile(manifest_member, io.BytesIO(manifest_data))
PY
if "${ADMIN}" restore "${malicious_archive}" --yes --install-root "${TEMP_ROOT}" >/dev/null 2>&1; then
  fail "restore accepted an archive containing a symbolic link"
fi

XG2G_ADMIN_SOURCE_DIR="${REPO_ROOT}" \
  "${ADMIN}" update --ref TEST-UPDATE --install-root "${TEMP_ROOT}" >/dev/null
[[ "$(tr -d '[:space:]' < "${TEMP_ROOT}/srv/xg2g/INSTALL_REF")" == "TEST-UPDATE" ]] ||
  fail "update did not record the target ref"
[[ "$(tr -d '[:space:]' < "${TEMP_ROOT}/var/lib/xg2g/admin/previous-ref")" == "HEAD" ]] ||
  fail "update did not retain rollback provenance"
XG2G_ADMIN_SOURCE_DIR="${REPO_ROOT}" \
  "${ADMIN}" rollback --yes --install-root "${TEMP_ROOT}" >/dev/null
[[ "$(tr -d '[:space:]' < "${TEMP_ROOT}/srv/xg2g/INSTALL_REF")" == "HEAD" ]] ||
  fail "rollback did not restore the previous ref"

"${REPO_ROOT}/infra/systemd/xg2g-admin.sh" uninstall --install-root "${TEMP_ROOT}" >/dev/null
[[ ! -e "${TEMP_ROOT}/srv/xg2g" ]] || fail "runtime tree survived uninstall"
[[ ! -e "${TEMP_ROOT}/usr/local/sbin/xg2g-admin" ]] ||
  fail "stable admin command survived uninstall"
[[ -f "${TEMP_ROOT}/var/lib/xg2g/sessions.sqlite" ]] ||
  fail "default uninstall removed persistent state"
"${REPO_ROOT}/infra/systemd/xg2g-admin.sh" uninstall --install-root "${TEMP_ROOT}" >/dev/null

echo "OK: Linux lifecycle commands are safe, restorable, and idempotent."
