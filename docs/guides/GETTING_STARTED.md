# Getting Started

This guide takes you from nothing to live TV in your browser. It assumes a home
or self-host setup; for production (HTTPS, systemd, Compose) see the
[Deployment Guide](../ops/DEPLOYMENT.md).

## Before you start

You need:

- **A Linux host with systemd**, on `amd64` or `arm64`.
- **Docker Engine with the Compose v2 plugin** on the host that will run xg2g.
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

## 1. Recommended: guided Linux installation

Open [GitHub Releases](https://github.com/ManuGH/xg2g/releases), download the
`linux_amd64` or `linux_arm64` archive plus `checksums.txt`, verify the
checksum, and extract the archive on the Linux server. Then run:

```bash
sudo ./infra/systemd/setup-linux.sh
```

Do not use **Code → Download ZIP** for installation. That ZIP represents a
mutable source branch and deliberately fails with a link back to Releases
instead of guessing which container image should be installed. A Git clone is
supported for development; the versioned release archive is the recommended
server installation.

The setup assistant uses plain-language questions for:

- the receiver address and optional OpenWebIF login,
- local, private/VPN, or public HTTPS access,
- the DVR rewind window, simultaneous streams, and verified free-space estimate,
- shared storage or an already-mounted HDD/SSD/NVMe path, and
- automatic, VAAPI, NVIDIA, or CPU-only transcoding.

It creates strong secrets, writes the protected configuration, installs the
self-contained systemd/Compose bundle from the exact release, enables daily
durable-state backups and runtime verification, and starts xg2g. It does not
partition, format, mount, or delete disks.

Choose the local-only option if you are only evaluating on the Linux host.
Phones, tablets, and other machines need HTTPS. At the HTTPS prompt, choose the
topology you actually operate:

| Choice | Use it when | What setup changes |
| :--- | :--- | :--- |
| Existing same-host proxy (default) | nginx, Traefik, Caddy, HAProxy, or another HTTPS proxy already forwards to xg2g | Keeps that proxy untouched. No Caddyfile, image pull, container, enable, or start; setup only validates the configured HTTPS URL and xg2g proxy trust. |
| Managed public Caddy (opt-in) | You have a public DNS name and want setup to own ACME HTTPS | Creates the xg2g Caddyfile and enables the pinned `xg2g-caddy.service`. |
| Managed internal Caddy (opt-in) | Access is limited to LAN/VPN and clients can trust a private CA | Creates the internal-CA Caddyfile, enables the service, and prints the CA certificate path. |

The Caddy unit file is shipped on every guided installation so a later switch
is deterministic, but it remains inactive without a setup-generated
`/etc/xg2g/Caddyfile`. Selecting an existing proxy never replaces, reloads, or
edits it.

Managed internal mode can bind Caddy to one exact LAN/VPN server IP instead of
all interfaces. The standard install deliberately binds xg2g itself only to
loopback, so it cannot be bypassed over cleartext from the LAN. Consequently,
the beginner wizard supports an existing proxy on the same Linux host. A proxy
on another machine requires an explicitly firewalled non-loopback backend
binding and is an advanced manual deployment.

For an existing proxy with a private CA, provide its CA certificate when asked.
Setup keeps a public copy at `/etc/xg2g/https-ca.crt` for later diagnostics and
does not finish until the configured URL serves the WebUI with HSTS and the
authenticated connectivity contract confirms effective HTTPS and the exact
allowed origin.

See the [current 2026 system overview](../arch/SYSTEM_OVERVIEW_2026.md) for the
complete runtime, storage, API, WebUI, and lifecycle boundaries.

If a required tool is missing, the installer prints the matching package
command for Debian/Ubuntu, Fedora/RHEL, or Arch. Docker Engine and its Compose
v2 plugin remain explicit prerequisites.

## 2. Alternative: one-container local evaluation

This command is intended for same-host evaluation. For a persistent production
installation, use the pinned, systemd-supervised workflow in
[Deployment](../ops/DEPLOYMENT.md); do not promote this container manually.

```bash
docker run -d --name xg2g --restart unless-stopped -p 127.0.0.1:8088:8088 \
  -e XG2G_E2_HOST="http://RECEIVER_IP" \
  -e XG2G_API_TOKEN="$(openssl rand -hex 32)" \
  -e XG2G_API_TOKEN_SCOPES="v3:admin" \
  -e XG2G_DECISION_SECRET="$(openssl rand -hex 32)" \
  ghcr.io/manugh/xg2g:v3.8.1
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

## 3. Confirm it is healthy

```bash
curl -fsS http://localhost:8088/readyz
docker logs xg2g
```

`readyz` returning OK means xg2g started and reached your receiver.

## 4. Open the WebUI

From the **same host**, open `http://localhost:8088/ui/` and sign in with the
`XG2G_API_TOKEN` you generated above.

## 5. Reaching xg2g from another device

Plain `http://` works **only from the same machine (loopback)**. From another
browser or device, xg2g rejects the browser session exchange
(`POST /api/v3/auth/session`) over plain HTTP, and playback will not start — by
design, because the session cookie must travel securely.

To use xg2g from your phone, tablet, or another PC, put it behind **HTTPS**: a
reverse proxy (Caddy, nginx, Traefik) that terminates TLS and forwards
`X-Forwarded-Proto: https`. See the [Deployment Guide](../ops/DEPLOYMENT.md).
Do not widen the Docker port to `8088:8088` when adding a reverse proxy; that
would leave a direct cleartext path around HTTPS.

## 6. Choose your channels

By default xg2g exposes your receiver's bouquets. Pick which one to serve with
`XG2G_BOUQUET`, and tune EPG, picons, and streaming policy as needed — all in
the [Configuration guide](CONFIGURATION.md#essential-start-here).

## Next steps

- `sudo xg2g-admin doctor` — verify Docker, storage,
  service health, and the published HTTPS endpoint.
- `sudo xg2g-admin backup` — create an immediate verified
  backup in `/var/backups/xg2g`.
- `sudo xg2g-admin update --ref vX.Y.Z` — back up, update,
  health-check, and automatically restore the previous ref if startup fails.
- [Configuration → Essential](CONFIGURATION.md#essential-start-here) — the core knobs.
- [Deployment Guide](../ops/DEPLOYMENT.md) — HTTPS, systemd, Compose, production.
- [Troubleshooting](TROUBLESHOOTING.md) — when something does not work.
