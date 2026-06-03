<!-- GENERATED FILE - DO NOT EDIT. Source: backend/templates/README.md.tmpl -->
# xg2g

[![CI](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/ci.yml)
[![Coverage](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml/badge.svg?branch=main)](https://github.com/ManuGH/xg2g/actions/workflows/coverage.yml)
[![Release](https://img.shields.io/github/v/release/ManuGH/xg2g)](https://github.com/ManuGH/xg2g/releases)
[![Go Report Card](https://goreportcard.com/badge/github.com/ManuGH/xg2g)](https://goreportcard.com/report/github.com/ManuGH/xg2g)
[![License](https://img.shields.io/badge/license-PolyForm%20NC-blue)](LICENSE)

<p align="center">
  <img src="docs/assets/github/xg2g-github-hero.svg" alt="xg2g turns Enigma2 MPEG-TS into browser-ready HLS for Safari, iPhone, iPad, Chrome, and modern TVs." width="100%" />
</p>

<div align="center">
  <strong>Self-hosted live-TV gateway for Enigma2.</strong><br />
  MPEG-TS in. Browser-ready HLS out. One server-side policy for Safari,
  iPhone, iPad, Chrome, TVs, recordings, and operator tooling.
  <br /><br />
  <a href="#quickstart"><strong>Quickstart</strong></a> ·
  <a href="https://manugh.github.io/xg2g/">API Docs</a> ·
  <a href="docs/README.md">Documentation</a> ·
  <a href="docs/arch/CODEC_MATRIX.md">Codec Matrix</a> ·
  <a href="https://github.com/ManuGH/xg2g/releases">Releases</a>
</div>

## What xg2g Does

| | |
| :--- | :--- |
| **Receiver bridge** | Resolves Enigma2/OpenWebIF streams, including receiver-selected relay ports such as `8001` or `17999`. |
| **Playback policy** | Chooses DirectPlay, DirectStream, or Transcode from source media, client capability, device policy, and runtime probes. |
| **Browser delivery** | Serves HLS with native `.ts` or fMP4 segments for Safari, iPhone, iPad, Chrome, desktop browsers, and TV clients. |
| **Operations surface** | Provides WebUI, `/api/v3`, health checks, metrics, structured logs, and deployment runbooks. |

## Playback Pipeline

```text
Enigma2 / OpenWebIF
  -> receiver-resolved stream URL
  -> xg2g decision engine
  -> HLS packaging or hardware transcode
  -> browser, phone, tablet, TV, or operator client
```

The decision engine evaluates **H.264, HEVC, AV1, MPEG-2, VP9** video and
**AAC, AC3, E-AC3, MP2, MP3** audio at runtime. When direct playback is not
safe, the universal fallback is **H.264 + AAC**. AV1/fMP4 is allowed only when
the browser runtime probe and device policy both prove that the client can
decode it safely.

[Read the codec/container matrix](docs/arch/CODEC_MATRIX.md)

## Quickstart

**Prerequisites:** Docker, `openssl`, and an Enigma2 receiver reachable on your
network

```bash
docker run -d --name xg2g --restart unless-stopped -p 8088:8088 \
  -e XG2G_E2_HOST="http://192.168.1.10" \
  -e XG2G_API_TOKEN="$(openssl rand -hex 32)" \
  -e XG2G_API_TOKEN_SCOPES="v3:admin" \
  -e XG2G_DECISION_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/manugh/xg2g:v3.5.1
```

Check the service health:

```bash
curl -fsS http://localhost:8088/readyz
```

Then open [http://localhost:8088/ui/](http://localhost:8088/ui/)

The published image is multi-architecture (`linux/amd64` and `linux/arm64`),
so xg2g runs on x86-64 servers and on arm64 hosts (Raspberry Pi, arm64 NAS).
Hardware transcoding (VAAPI/NVENC) is x86-only; on arm64, ffmpeg uses software
encoding.

> `XG2G_DECISION_SECRET` is mandatory. xg2g refuses to start without a live
> playback signing secret.
>
> Local quickstart over `http://localhost:8088/ui/` is supported only from the
> same host. If you access xg2g from another browser or device, you must expose
> it via HTTPS or a trusted HTTPS proxy. Browser playback needs
> `POST /api/v3/auth/session` to mint the `xg2g_session` cookie, and that
> exchange is rejected over plain HTTP for non-loopback clients.

**Next steps:**
[Documentation](docs/README.md) •
[Configuration](docs/guides/CONFIGURATION.md) •
[Deployment](docs/ops/DEPLOYMENT.md) •
[Security](docs/ops/SECURITY.md) •
[Architecture](docs/arch/README.md) •
[ADRs](docs/ADR/)

**Local development:**

```bash
make install
make dev-tools
make doctor
make start
```

Optional host bootstrap: `mise install` uses [mise.toml](mise.toml) to install
the pinned Go and Node versions. Optional containerized bootstrap: reopen the
repo in [.devcontainer/devcontainer.json](.devcontainer/devcontainer.json).

Then switch to `make start-gpu`, `make start-nvidia`, or `make dev-ui` only
when you explicitly need a hardware-specific container path or frontend HMR.

If you are new to the repository layout, read the
[Repository Map](docs/dev/REPO_MAP.md) before editing. Local runtime outputs
such as `data/`, `logs/`, `artifacts/`, `test-results/`, `node_modules/`,
`.venv/`, and `bin/` are intentionally ignored and should not be committed.

## Status

| Component | Status | Guarantee |
| :--- | :--- | :--- |
| **API** | Stable (v3) | SemVer |
| **WebUI** | Stable | Thin Client |
| **Streaming** | Production | Universal Policy |
| **FFmpeg** | Pinned (8.1.1) | Bundled in Docker image |

Structured logs, Prometheus metrics, OpenTelemetry traces, fail-closed auth,
Docker health checks, and CI-backed release automation are built in.

## Documentation

| | |
| :--- | :--- |
| **Get started** | [Documentation Portal](docs/README.md) · [10-Minute Intro](backend/NEW_HERE.md) · [Repository Map](docs/dev/REPO_MAP.md) · [API Reference](https://manugh.github.io/xg2g/) |
| **Operate** | [Ops Index](docs/ops/README.md) · [Configuration](docs/guides/CONFIGURATION.md) · [Deployment](docs/ops/DEPLOYMENT.md) · [Client Profiles](docs/ops/CLIENT_PROFILES.md) · [Security](docs/ops/SECURITY.md) |
| **Develop** | [Dev Index](docs/dev/README.md) · [Architecture Index](docs/arch/README.md) · [Codec Matrix](docs/arch/CODEC_MATRIX.md) · [WebUI Index](docs/webui/README.md) · [CI Playbook](docs/ops/CI_FAILURE_PLAYBOOK.md) |

## License

[PolyForm Noncommercial 1.0.0](LICENSE)

- Free for personal, homelab, and educational use.
- Commercial use requires permission.
