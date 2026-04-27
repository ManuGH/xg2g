# Smoke Test

Use this page as the quick smoke-test router. The canonical operator-grade
matrix lives in [Service Smoke](SERVICE_SMOKE.md).

## Production Or Staging Host

Preferred path after a deploy:

```bash
sudo /srv/xg2g/scripts/run-service-smoke.sh
sudo /srv/xg2g/scripts/verify-post-deploy-playback.sh
```

The post-deploy verifier proves auth-gated HLS, DVR manifest semantics, and
hardware-transcode reachability where expected.

## Local Container Sanity Check

This is only a container boot sanity check. It does not replace the service
smoke matrix and it does not prove real receiver playback.

```bash
token="$(openssl rand -hex 32)"
decision_secret="$(openssl rand -hex 32)"

docker run -d --rm --name xg2g-smoke -p 8088:8088 \
  -e XG2G_E2_HOST="http://192.168.1.50" \
  -e XG2G_API_TOKEN="$token" \
  -e XG2G_API_TOKEN_SCOPES="v3:admin" \
  -e XG2G_DECISION_SECRET="$decision_secret" \
  ghcr.io/manugh/xg2g:v3.4.9

curl -fsS http://localhost:8088/healthz
curl -fsS -H "Authorization: Bearer $token" \
  http://localhost:8088/api/v3/system/healthz

if docker logs xg2g-smoke 2>&1 | grep -Ei "panic|fatal"; then
  echo "unexpected panic/fatal log output" >&2
  docker stop xg2g-smoke
  false
fi
docker stop xg2g-smoke
```

Expected result:

- `/healthz` responds.
- Authenticated `/api/v3/system/healthz` responds.
- Logs contain no panic/fatal messages.

## What Not To Use

- Do not use old `xg2g:3.1.5` examples.
- Do not run without `XG2G_DECISION_SECRET`; live playback startup requires it.
- Do not treat unauthenticated `/api/v3/*` success as valid. Control-plane API
  calls must require auth.
