# Troubleshooting

Most problems are one of three things: a missing required secret, the HTTPS
requirement for remote access, or the receiver not being reachable.

## Container exits immediately

Check the logs:

```bash
docker logs xg2g
```

- **`XG2G_DECISION_SECRET is required but not set`** — pass a signing secret;
  xg2g refuses to start without one. Generate with `openssl rand -hex 32`.
- **Missing `XG2G_E2_HOST`** — set the receiver base URL.

## Playback fails, or the WebUI won't log in from another device

The most common report. Plain `http://` is accepted **only from the same host
(loopback)**. From any other browser or device, the session exchange
(`POST /api/v3/auth/session`) is rejected over plain HTTP with `HTTPS Required`,
and playback can't start.

**Fix:** serve xg2g over **HTTPS** via a reverse proxy that terminates TLS and
forwards `X-Forwarded-Proto: https` (Caddy, nginx, Traefik). See the
[Deployment Guide](../ops/DEPLOYMENT.md). Local testing on the same machine over
`http://localhost:8088/ui/` keeps working.

## No channels, or receiver errors

- Confirm the receiver answers OpenWebIF:
  `curl -fsS http://RECEIVER_IP/api/about`.
- Check `XG2G_E2_HOST` points at the receiver (scheme + host, e.g.
  `http://192.168.1.10`).
- If the receiver requires an OpenWebIF login, set `XG2G_E2_USER` and
  `XG2G_E2_PASS`.
- Pick a bouquet with `XG2G_BOUQUET` (see
  [Configuration](CONFIGURATION.md#essential-start-here)).

## Black screen, audio-only, or "session expired" during playback

xg2g chooses direct play, remux, or transcode from the source codec and your
browser's capabilities. Edge cases (for example AV1 on a device that can't
decode it) can show a black frame or stop playback. The rules are in the
[Codec Matrix](../arch/CODEC_MATRIX.md) and
[Client Profiles](../ops/CLIENT_PROFILES.md). When reporting, include the
browser/device and the channel.

## arm64 host: transcoding is slow or fails

Hardware transcoding (VAAPI/NVENC) is **x86-only**. On arm64 (Raspberry Pi,
arm64 NAS) xg2g runs ffmpeg in software, which is much heavier. Prefer channels
that direct-play or remux, or run on x86-64 if you need hardware transcode.

## Where to look

- Logs: `docker logs xg2g` (structured JSON; set `XG2G_LOG_LEVEL=debug` for more).
- Health: `curl -fsS http://localhost:8088/readyz`.
- Metrics: the Prometheus endpoint (see [Operations Index](../ops/README.md)).
