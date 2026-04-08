# Service Smoke Matrix (Operator-Grade 2026)

This is a short, CTO-grade smoke matrix for the hardened systemd + Compose unit.
Run as root (or with sudo) on the host.

## Preconditions
- Unit installed at `/etc/systemd/system/xg2g.service`
- Compose file at `/srv/xg2g/docker-compose.yml`
- Optional `/dev/dri` overlay at `/srv/xg2g/docker-compose.gpu.yml`
- Optional NVIDIA overlay at `/srv/xg2g/docker-compose.nvidia.yml`
- Env file at `/etc/xg2g/xg2g.env`

## Quick Run (Canonical)
Run the deterministic smoke runner (preferred):
```bash
sudo /srv/xg2g/scripts/run-service-smoke.sh
```

After a real deploy, run the playback verifier as a second step:
```bash
sudo /srv/xg2g/scripts/verify-post-deploy-playback.sh
```

On NVIDIA-only hosts, you can pin the expected hardware backend explicitly:
```bash
sudo XG2G_POST_DEPLOY_EXPECT_ENCODER_BACKEND=nvenc /srv/xg2g/scripts/verify-post-deploy-playback.sh
```

The script implements the matrix below; use the manual steps for troubleshooting.

## Matrix

1) Missing compose file -> start must fail
```bash
systemctl stop xg2g || true
cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh down --remove-orphans || true
mv /srv/xg2g/docker-compose.yml /srv/xg2g/docker-compose.yml.bak
systemctl start xg2g || true
systemctl status xg2g --no-pager
result="$(systemctl show -p Result --value xg2g)"
case "$result" in
  condition) echo "OK: condition failed as expected" ;;
  failed|exit-code|resources) echo "OK: failed as expected" ;;
  *) echo "Expected condition/failed, got: $result" >&2; exit 1 ;;
esac
mv /srv/xg2g/docker-compose.yml.bak /srv/xg2g/docker-compose.yml
cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g
test -z "$(cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g)"
systemctl reset-failed xg2g
```
Expected: systemd fails on `ConditionPathExists`; `compose-xg2g.sh ps -q xg2g` returns empty.

2) Missing env file -> start must fail
```bash
systemctl stop xg2g || true
cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh down --remove-orphans || true
mv /etc/xg2g/xg2g.env /etc/xg2g/xg2g.env.bak
systemctl start xg2g || true
systemctl status xg2g --no-pager
result="$(systemctl show -p Result --value xg2g)"
case "$result" in
  failed|exit-code|resources) echo "OK: failed as expected" ;;
  *) echo "Expected failed/exit-code/resources, got: $result" >&2; exit 1 ;;
esac
mv /etc/xg2g/xg2g.env.bak /etc/xg2g/xg2g.env
cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g
test -z "$(cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g)"
systemctl reset-failed xg2g
```
Expected: systemd fails on `EnvironmentFile`; `compose-xg2g.sh ps -q xg2g` returns empty.

3) Whitespace token -> start must fail (fail-closed)
```bash
systemctl stop xg2g || true
cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh down --remove-orphans || true
cp /etc/xg2g/xg2g.env /etc/xg2g/xg2g.env.bak
awk -F= '$1 != "XG2G_API_TOKEN" { print } END { print "XG2G_API_TOKEN=\"   \"" }' /etc/xg2g/xg2g.env > /etc/xg2g/xg2g.env.tmp
mv /etc/xg2g/xg2g.env.tmp /etc/xg2g/xg2g.env
systemctl start xg2g || true
systemctl status xg2g --no-pager
result="$(systemctl show -p Result --value xg2g)"
case "$result" in
  failed|exit-code|resources) echo "OK: failed as expected" ;;
  *) echo "Expected failed/exit-code/resources, got: $result" >&2; exit 1 ;;
esac
mv /etc/xg2g/xg2g.env.bak /etc/xg2g/xg2g.env
cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g
test -z "$(cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g)"
systemctl reset-failed xg2g
```
Expected: start fails on trim check; `compose-xg2g.sh ps -q xg2g` returns empty.

4) Valid env -> start must succeed
```bash
systemctl start xg2g
cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps
cid="$(cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g)"
test -n "$cid"
docker inspect --format '{{.State.Health.Status}}' "$cid"
```
Expected: `compose-xg2g.sh ps -q xg2g` returns non-empty and health is `healthy`.

5) Reload is idempotent (no down/up flap)
```bash
cid_before="$(cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g)"
systemctl reload xg2g
cid_after="$(cd /srv/xg2g && /srv/xg2g/scripts/compose-xg2g.sh ps -q xg2g)"
test "$cid_before" = "$cid_after" && echo "OK: no container recreation"
```
Expected: container ID unchanged when there are no config changes.
