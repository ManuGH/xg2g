# AGENTS.md

## Ops Triage Truth

For `xg2g` start/restart incidents, do not assume checked-in docs match the live host.
Capture and compare these three files before patching anything:

- `/etc/systemd/system/xg2g.service` — installed unit that systemd actually runs
- `/srv/xg2g/docker-compose.yml` — frozen base Compose source of truth for the `xg2g` service image
- `/srv/xg2g/docker-compose.gpu.yml` — optional GPU overlay; compare it too when present
- `/etc/xg2g/xg2g.env` — live environment file loaded by both systemd and Compose; may also select compose files via `COMPOSE_FILE`

The checked-in canonical unit is [deploy/xg2g.service](/root/xg2g/deploy/xg2g.service), rendered from [backend/templates/docs/ops/xg2g.service.tmpl](/root/xg2g/backend/templates/docs/ops/xg2g.service.tmpl). The live unit may drift from both the repo truth and the deployed host copy under `/srv/xg2g/docs/ops/xg2g.service`.

## Restart Failure Order

Run these first:

```bash
systemctl status xg2g.service --no-pager -l
journalctl -xeu xg2g.service --no-pager -n 120
docker inspect -f '{{.State.Status}} {{if .State.Health}}{{.State.Health.Status}}{{else}}no-health{{end}}' xg2g
docker logs --since 5m xg2g
```

Then classify the failure before editing:

- `ExecStartPre` fails with `No such image`: the live unit is likely checking a stale hardcoded tag. Treat `services.xg2g.image` in `/srv/xg2g/docker-compose.yml` as image truth, not an old registry tag.
- Container logs fail with `XG2G_DECISION_SECRET is required but not set`: `/etc/xg2g/xg2g.env` is missing a mandatory live-stream signing secret. Required length is at least 32 ASCII bytes; see [docs/ops/SECURITY.md](/root/xg2g/docs/ops/SECURITY.md).
- `systemctl start` or `restart` fails at `ExecStartPost` with `Container is unhealthy`: inspect Docker health details, not just `/readyz`.

## Health Nuance

`/readyz` can return `200` while Docker health is still `unhealthy`.
When that happens, inspect the container health log directly:

```bash
docker inspect -f '{{json .State.Health}}' xg2g
```

One confirmed failure mode is metrics-only health drift:

- readiness endpoint is healthy
- Docker healthcheck still fails because `http://localhost:9091/metrics` is unreachable

That symptom means the service is running far enough to answer readiness, but systemd will still fail the start because Docker health never turns green.

## Documentation Rule

If the repo template, the checked-in runbook, and the live host disagree, update [docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md](/root/xg2g/docs/ops/RUNBOOK_SYSTEMD_COMPOSE.md) with the exact observed delta before doing larger cleanup work.
