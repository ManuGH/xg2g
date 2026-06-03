# Getting Started

This guide takes you from nothing to live TV in your browser. It assumes a home
or self-host setup; for production (HTTPS, systemd, Compose) see the
[Deployment Guide](../ops/DEPLOYMENT.md).

## Before you start

You need:

- **Docker** on the host that will run xg2g.
- **An Enigma2 receiver** (e.g. a VU+ or Dreambox running OpenWebIF) reachable
  on your network. Confirm it answers:

  ```bash
  curl -fsS http://RECEIVER_IP/api/about
  ```

  If that fails, fix receiver reachability first — xg2g talks to it over
  OpenWebIF.
- **`openssl`**, or any way to generate random secrets.
- **Enough CPU for transcoding.** xg2g always runs FFmpeg to package the
  receiver's stream into HLS for the browser; when the source isn't browser-safe
  (usually — MPEG-2/HEVC video or AC3/MP2 audio), it also transcodes to H.264/AAC,
  which is the CPU-heavy part. On x86 you can offload video encoding to a GPU/iGPU
  (VAAPI/NVENC); arm64 is software-only and much heavier. Plan for meaningful CPU
  (or a GPU encode session) per concurrent stream.

## 1. Start xg2g

```bash
docker run -d --name xg2g --restart unless-stopped -p 8088:8088 \
  -e XG2G_E2_HOST="http://RECEIVER_IP" \
  -e XG2G_API_TOKEN="$(openssl rand -hex 32)" \
  -e XG2G_API_TOKEN_SCOPES="v3:admin" \
  -e XG2G_DECISION_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/manugh/xg2g:v3.5.1
```

What each setting does:

- `XG2G_E2_HOST` — base URL of your receiver. **Required.**
- `XG2G_API_TOKEN` / `XG2G_API_TOKEN_SCOPES` — the token for the API and WebUI.
  `v3:admin` grants full access.
- `XG2G_DECISION_SECRET` — signs live-stream playback tokens. **Required** —
  xg2g refuses to start without it.

If your receiver requires an OpenWebIF login, also set `XG2G_E2_USER` and
`XG2G_E2_PASS`.

The image is multi-arch (`linux/amd64` + `linux/arm64`), so it runs on x86-64
servers and on arm64 boards/NAS. Hardware transcoding (VAAPI/NVENC) is x86-only;
on arm64, ffmpeg uses software encoding.

## 2. Confirm it is healthy

```bash
curl -fsS http://localhost:8088/readyz
docker logs xg2g
```

`readyz` returning OK means xg2g started and reached your receiver.

## 3. Open the WebUI

From the **same host**, open `http://localhost:8088/ui/` and sign in with the
`XG2G_API_TOKEN` you generated above.

## 4. Reaching xg2g from another device

Plain `http://` works **only from the same machine (loopback)**. From another
browser or device, xg2g rejects the browser session exchange
(`POST /api/v3/auth/session`) over plain HTTP, and playback will not start — by
design, because the session cookie must travel securely.

To use xg2g from your phone, tablet, or another PC, put it behind **HTTPS**: a
reverse proxy (Caddy, nginx, Traefik) that terminates TLS and forwards
`X-Forwarded-Proto: https`. See the [Deployment Guide](../ops/DEPLOYMENT.md).

## 5. Choose your channels

By default xg2g exposes your receiver's bouquets. Pick which one to serve with
`XG2G_BOUQUET`, and tune EPG, picons, and streaming policy as needed — all in
the [Configuration guide](CONFIGURATION.md#essential-start-here).

## Next steps

- [Configuration → Essential](CONFIGURATION.md#essential-start-here) — the core knobs.
- [Deployment Guide](../ops/DEPLOYMENT.md) — HTTPS, systemd, Compose, production.
- [Troubleshooting](TROUBLESHOOTING.md) — when something does not work.
