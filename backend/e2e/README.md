# WebUI e2e harness

Browser-real end-to-end tests for the xg2g WebUI: Playwright drives a real
Chromium against the production WebUI bundle (served by `vite`), with all
backend calls answered by a local **Fastify fixture-server**. No real backend,
no production deployment, no network egress.

## Topology / where this runs

- **Do NOT run this on the Proxmox host (`/root`).** The host is the bare
  hypervisor; it has no docker and must stay lean (no browsers, no Playwright
  download).
- Run it in the **docker lab (LXC 110)** or in **CI**. Everything is
  containerized so the host stays untouched.

## One-command run

Build context is the **repo root** (the image needs `frontend/webui`, the
fixture-server, and the e2e sources):

```sh
docker build -f backend/e2e/Dockerfile.e2e -t xg2g-e2e .
docker run --rm xg2g-e2e
```

The image:
1. starts from the pinned Playwright base (`v1.58.0-jammy`, bundled browsers),
2. installs `ffmpeg` and generates a ~4s HLS VOD fixture at build time
   (`scripts/gen-hls-fixture.sh` → `fixtures/hls/`), so no media binary is
   committed,
3. runs `npx playwright test -c backend/e2e/playwright.config.ts`.

`playwright.config.ts` starts both web servers itself (`webServer` block): the
fixture-server on `:3001` and `vite` on `:4173` with the dev proxy pointed at
the fixture-server.

## Reproducing on the Proxmox topology (host repo → LXC 110)

The two commands above assume the repo is *on the machine that has docker*. In
this homelab it is not: the repo lives on the **bare Proxmox host** (`/root/xg2g`,
no docker), while docker runs in **LXC 110** — and the repo is **not** bind-mounted
into 110. So a from-scratch run on 110 has a source-transfer step first. From the
Proxmox host:

```sh
# 1. ship the source into 110 (exclude node_modules/.git — the image runs npm ci)
tar czf /tmp/xg2g-e2e.tar.gz --exclude=node_modules --exclude=.git \
  --exclude='backend/e2e/fixtures/hls' -C /root xg2g
pct push 110 /tmp/xg2g-e2e.tar.gz /tmp/xg2g-e2e.tar.gz
pct exec 110 -- sh -c 'rm -rf /tmp/xg2g-build && mkdir -p /tmp/xg2g-build \
  && tar xzf /tmp/xg2g-e2e.tar.gz -C /tmp/xg2g-build'

# 2. build + run inside 110 (.dockerignore trims the build context)
pct exec 110 -- sh -c 'cd /tmp/xg2g-build/xg2g \
  && docker build -f backend/e2e/Dockerfile.e2e -t xg2g-e2e . \
  && docker run --rm xg2g-e2e'
```

**Re-running** is cheaper: the built image is self-contained (source + HLS fixture
are baked in at build time), so a pure re-run is just
`pct exec 110 -- docker run --rm xg2g-e2e` — no re-push, no rebuild. **But** that
image is a frozen snapshot: after you change anything under `frontend/webui` or
`backend/e2e`, you must re-push (step 1) and rebuild (step 2), or you are testing
stale code. To capture the Playwright trace on a failure (it lives inside the
container and `--rm` discards it), mount a volume:
`docker run --rm -v /tmp/xg2g-out:/app/test-results xg2g-e2e`.

> Verified green on LXC 110, 2026-05-24 — 2/2 (boot smoke + real-hls.js playback,
> `currentTime` advancing).

## CI

This suite is a **required PR gate**: `pr-required-gates.yml` runs
`make webui-browser-smoke` whenever a PR touches `frontend/webui/` or
`backend/e2e/`. The make target runs *all* specs in `specs/` (no filter), so any
spec you add here runs on every relevant PR — including the real-hls.js playback
spec. The CI job installs `ffmpeg` and the make target runs `gen-hls-fixture.sh`,
so the fixture exists in CI and the playback spec actually runs (it does **not**
silently skip). If `ffmpeg` is ever missing, `gen-hls-fixture.sh` is non-fatal
and the playback spec skips itself rather than failing the whole gate — but CI is
configured so that path should not trigger.

## Layout

```
backend/e2e/
  Dockerfile.e2e            # the only supported way to run (isolated lab/CI)
  playwright.config.ts      # chromium-only; starts fixture-server + vite
  fixture-server/server.js  # Fastify mock; scenario switch via POST /__admin/scenario
  scenarios/*.scenario.json # per-test backend responses (exact-path match)
  scenario.schema.json      # documents the scenario shape (not enforced by a runner)
  scripts/gen-hls-fixture.sh# ffmpeg HLS fixture generator (container build only)
  specs/                    # Playwright specs
  fixtures/hls/             # generated at build time; gitignored
```

## How a scenario works

The fixture-server holds one **active scenario** at a time. A spec selects it in
`beforeEach`:

```ts
await request.post('http://127.0.0.1:3001/__admin/scenario', { data: { id: 'minimal-boot' } });
```

Each scenario lists `endpoints` matched by **exact path + method**
(`request.url === e.path`). Query strings are part of the match
(`/api/v3/services?bouquet=Favorites`). Unknown paths return
`404 {"error":"Mock path not defined in scenario"}` — watch the server log for
these when authoring a new scenario.

### HLS playback

`fixtures/hls/index.m3u8` + segments are served by a dedicated static route at
`/api/v3/sessions/:sessionId/hls/:filename` (any session id). The path lives
under `/api/v3/` and is same-origin, satisfying the player's origin guard
(`useLiveSessionController.ts`) and the `HEAD` pre-flight.

## Specs

- `minimal-boot` scenario — boot/shell smoke.
- `playback.spec.ts` (`playback-live` scenario) — **real hls.js** live playback:
  clicks the channel CTA, waits for `<video>`, kicks muted playback, and asserts
  `currentTime` advances past 0.2s with no media error. This is the one thing the
  jsdom unit tests cannot prove: that frames actually decode.

> **playback-live contract** is verified against `usePlaybackOrchestrator`,
> `useLiveSessionController` and the generated client types: `POST
> /live/stream-info` (returns `mode` + `playbackDecisionToken`) → `POST
> /intents` (`202` `IntentAcceptedResponse`) → `GET /sessions/{id}` (polled to
> `READY` with `playbackUrl`) → `heartbeat`. The decision token is **mandatory**
> for live — the orchestrator fails closed without it. On the **first lab run**,
> still watch the fixture-server log for any `/api/v3/*` path returning 404 in
> case a field drifts. Known limitation: the exact-path matcher cannot
> distinguish `stream.start` from `stream.stop` (both are `POST /intents`).
